package interfaces

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
)

// nodeIDRegex matches standard OPC UA node ID strings in the form
// `ns=<index>;<s|i|b|g>=<identifier>`.
var nodeIDRegex = regexp.MustCompile(`^ns=\d+;[sibg]=.*$`)

// SubscriptionMapping binds a monitored OPC UA node to a specific control
// element (touch field, knob, or button) and attribute ("nodeid", "opcua", or
// "blink") in a Loupedeck profile.
type SubscriptionMapping struct {
	Field      string `json:"field,omitempty"`
	ButtonID   int    `json:"buttonID"`
	ButtonName string `json:"buttonName"`
	Attribute  string `json:"attribute"`
}

// OPCUANodeSnapshot captures the current cached or simulated value and all
// associated control mappings for a single monitored OPC UA node.
type OPCUANodeSnapshot struct {
	NodeID   string                `json:"nodeId"`
	Value    interface{}           `json:"value"`
	Mappings []SubscriptionMapping `json:"mappings"`
}

// OPCUASubscriptionsSnapshot represents a point-in-time view of the OPC UA
// interface connection state, dry-run mode, and all monitored nodes.
type OPCUASubscriptionsSnapshot struct {
	EndpointURL string              `json:"endpointUrl"`
	Connected   bool                `json:"connected"`
	DryRun      bool                `json:"dryRun"`
	Nodes       []OPCUANodeSnapshot `json:"nodes"`
}

// OPCUAIf manages an OPC UA client session, node subscriptions, data-change
// notifications, and typed node writes.
//
// Concurrency and lifecycle:
//   - Subscription metadata, cached/simulated values, type cache, and handle
//     lookups are synchronized via subMu.
//   - The active gopcua client, subscription, and lifecycle context are guarded
//     by clientMu, while connected and nextHdl use atomic operations.
//   - Session establishment runs asynchronously with automatic retry in
//     connectAndSubscribeLoop; Init may reuse an existing connected session when
//     the endpoint URL is unchanged.
type OPCUAIf struct {
	BaseIf
	subMu                  sync.RWMutex
	endpointURL            string
	publishingInterval     int
	samplingInterval       int
	desiredSubscriptions   map[string][]SubscriptionMapping
	desiredOrder           []string
	simulatedValues        map[string]interface{}
	types                  map[string]ua.TypeID
	nodeByHandle           map[uint32]string
	OnMonitoredItemChanged MonitoredItemChangedFunc

	clientMu  sync.Mutex
	client    *opcua.Client
	sub       *opcua.Subscription
	ctx       context.Context
	cancel    context.CancelFunc
	connected atomic.Bool
	nextHdl   atomic.Uint32
}

// NewOPCUAIf creates and returns an initialized OPCUAIf with empty subscription,
// simulated-value, data-type, and handle-lookup maps.
func NewOPCUAIf() *OPCUAIf {
	return &OPCUAIf{
		desiredSubscriptions: make(map[string][]SubscriptionMapping),
		simulatedValues:      make(map[string]interface{}),
		types:                make(map[string]ua.TypeID),
		nodeByHandle:         make(map[uint32]string),
	}
}

// IsNodeID reports whether nodeID matches the OPC UA node ID syntax
// (`ns=<namespace>;<s|i|b|g>=<identifier>`).
func (o *OPCUAIf) IsNodeID(nodeID string) bool {
	return nodeIDRegex.MatchString(nodeID)
}

// SetOnMonitoredItemChanged registers fn as the callback invoked whenever a
// monitored OPC UA node (or simulated node value) changes. It acquires BaseIf.Mu.
func (o *OPCUAIf) SetOnMonitoredItemChanged(fn MonitoredItemChangedFunc) {
	o.Mu.Lock()
	defer o.Mu.Unlock()
	o.OnMonitoredItemChanged = fn
}

// emitMonitoredChanged safely reads OnMonitoredItemChanged under BaseIf.Mu and
// invokes it outside the lock if non-nil.
func (o *OPCUAIf) emitMonitoredChanged(buttonID, attribute, nodeID string, value interface{}, field string) {
	o.Mu.RLock()
	fn := o.OnMonitoredItemChanged
	o.Mu.RUnlock()
	if fn != nil {
		fn(buttonID, attribute, nodeID, value, field)
	}
}

// GetSubscriptionsSnapshot returns a thread-safe deep copy of the configured
// OPC UA endpoint, connection and dry-run status, and ordered monitored nodes
// with their latest cached or simulated values and control mappings.
func (o *OPCUAIf) GetSubscriptionsSnapshot() OPCUASubscriptionsSnapshot {
	o.subMu.RLock()
	defer o.subMu.RUnlock()

	nodes := make([]OPCUANodeSnapshot, 0, len(o.desiredOrder))
	for _, nodeID := range o.desiredOrder {
		mappings := o.desiredSubscriptions[nodeID]
		mappingsCopy := make([]SubscriptionMapping, len(mappings))
		copy(mappingsCopy, mappings)
		var val interface{}
		if v, exists := o.simulatedValues[nodeID]; exists {
			val = v
		}
		nodes = append(nodes, OPCUANodeSnapshot{
			NodeID:   nodeID,
			Value:    val,
			Mappings: mappingsCopy,
		})
	}
	return OPCUASubscriptionsSnapshot{
		EndpointURL: o.endpointURL,
		Connected:   o.connected.Load(),
		DryRun:      o.GetDryRun(),
		Nodes:       nodes,
	}
}

// SimulateNodeValue injects value for nodeID into the simulated-value cache,
// notifies all subscribed control mappings via OnMonitoredItemChanged, emits a
// simulated "opcua-sub" ActivityEntry, and returns a slice of formatted target
// labels (e.g. "field.button:attribute") that received the update.
func (o *OPCUAIf) SimulateNodeValue(nodeID string, value interface{}) []string {
	o.subMu.Lock()
	o.simulatedValues[nodeID] = value
	mappings := o.desiredSubscriptions[nodeID]
	mappingsCopy := make([]SubscriptionMapping, len(mappings))
	copy(mappingsCopy, mappings)
	o.subMu.Unlock()

	pushedTo := make([]string, 0, len(mappingsCopy))
	for _, m := range mappingsCopy {
		targetButton := m.ButtonName
		if targetButton == "" {
			targetButton = strconv.Itoa(m.ButtonID)
		}
		label := fmt.Sprintf("%s:%s", targetButton, m.Attribute)
		if m.Field != "" {
			label = fmt.Sprintf("%s.%s:%s", m.Field, targetButton, m.Attribute)
		}
		pushedTo = append(pushedTo, label)
		o.emitMonitoredChanged(targetButton, m.Attribute, nodeID, value, m.Field)
	}

	valJSON, _ := json.Marshal(value)
	o.LogInfo(fmt.Sprintf("OPCUAIf [Simulated Subscription]: %s = %v -> [%s]\n", nodeID, value, strings.Join(pushedTo, ", ")))
	updatedLabel := strings.Join(pushedTo, ", ")
	if updatedLabel == "" {
		updatedLabel = "none"
	}
	o.EmitActivity(ActivityEntry{
		Timestamp: time.Now().UnixMilli(),
		Interface: "opcua-sub",
		Status:    "ok",
		Simulated: true,
		Raw:       nodeID,
		Formatted: fmt.Sprintf("%s = %s", nodeID, string(valJSON)),
		ButtonID:  strings.Join(pushedTo, ", "),
		State:     config.ToString(value),
		Event:     "subscription",
		Message:   fmt.Sprintf("Simulated OPC UA node %s => %v (updated: %s)", nodeID, value, updatedLabel),
	})
	return pushedTo
}

// SetDryRun enables or disables dry-run mode for the OPC UA interface.
// Switching into dry-run mode disconnects any active OPC UA session. Switching
// out of dry-run mode when an endpoint URL is configured starts an asynchronous
// connection and subscription loop and waits up to 250ms for initial readiness.
func (o *OPCUAIf) SetDryRun(enabled bool) {
	wasDry := o.GetDryRun()
	o.BaseIf.SetDryRun(enabled)
	if enabled && !wasDry {
		_ = o.Disconnect()
		return
	}
	if !enabled && wasDry {
		o.subMu.RLock()
		endpointURL := o.endpointURL
		o.subMu.RUnlock()
		if endpointURL != "" && !o.connected.Load() {
			o.clientMu.Lock()
			if o.cancel != nil {
				o.cancel()
			}
			o.ctx, o.cancel = context.WithCancel(context.Background())
			ctx := o.ctx
			o.clientMu.Unlock()
			readyCh := make(chan struct{})
			go o.connectAndSubscribeLoop(ctx, endpointURL, readyCh)
			select {
			case <-readyCh:
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
}

// cancelSubscriptionOnly cancels the active context and tears down the current
// OPC UA subscription (with a 1-second timeout) while keeping the underlying
// OPC UA client session alive for reuse.
func (o *OPCUAIf) cancelSubscriptionOnly() {
	o.clientMu.Lock()
	defer o.clientMu.Unlock()
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
	if o.sub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_ = o.sub.Cancel(ctx)
		cancel()
		o.sub = nil
	}
}

// Init configures the OPC UA interface from profile parameters in options and
// collects all monitored node subscriptions declared in prof.
//
// Behavior and lifecycle:
//   - Requires "endpointurl" in options; logs an error and returns early if missing.
//   - Reads optional "publishingInterval" (default 250ms) and "samplingInterval"
//     (default 100ms).
//   - If already connected in live mode to the same endpoint URL, reuses the
//     existing client session and re-registers subscriptions; otherwise
//     disconnects any prior session.
//   - Re-emits any previously cached/simulated values for matching nodes so UI
//     states persist across profile switches.
//   - In dry-run mode, logs initialization without connecting. In live mode,
//     starts connectAndSubscribeLoop (or registerAllSubscriptions when reusing
//     a session) and waits up to 250ms for initial readiness.
func (o *OPCUAIf) Init(options map[string]interface{}, prof *config.Profile) {
	if options == nil {
		o.LogError("OPCUAIf: Missing endpointurl in options => Skipping OPCUA init\n")
		return
	}
	rawEndpoint, hasEndpoint := options["endpointurl"]
	if !hasEndpoint || rawEndpoint == nil {
		o.LogError("OPCUAIf: Missing endpointurl in options => Skipping OPCUA init\n")
		return
	}

	endpointURL := o.FormatString(config.ToString(rawEndpoint), options, nil)
	pubInterval := config.ToIntDefault(options["publishingInterval"], 250)
	sampInterval := config.ToIntDefault(options["samplingInterval"], 100)

	o.subMu.RLock()
	prevEndpoint := o.endpointURL
	o.subMu.RUnlock()

	o.clientMu.Lock()
	existingClient := o.client
	o.clientMu.Unlock()

	reuseSession := !o.GetDryRun() && o.connected.Load() && existingClient != nil && prevEndpoint == endpointURL
	if reuseSession {
		o.cancelSubscriptionOnly()
	} else {
		_ = o.Disconnect()
	}

	o.subMu.Lock()
	o.endpointURL = endpointURL
	o.publishingInterval = pubInterval
	o.samplingInterval = sampInterval
	o.desiredSubscriptions = make(map[string][]SubscriptionMapping)
	o.desiredOrder = nil
	o.nodeByHandle = make(map[uint32]string)
	o.subMu.Unlock()

	o.Mu.Lock()
	o.Options = config.CloneMap(options)
	o.Mu.Unlock()

	o.collectProfileSubscriptions(options, prof)

	// Re-apply any already simulated/cached values so profile switches keep current states
	o.subMu.RLock()
	orderCopy := append([]string(nil), o.desiredOrder...)
	o.subMu.RUnlock()
	for _, nodeID := range orderCopy {
		o.subMu.RLock()
		val, hasVal := o.simulatedValues[nodeID]
		mappings := append([]SubscriptionMapping(nil), o.desiredSubscriptions[nodeID]...)
		o.subMu.RUnlock()
		if hasVal {
			for _, m := range mappings {
				o.emitMonitoredChanged(m.ButtonName, m.Attribute, nodeID, val, m.Field)
			}
		}
	}

	if o.GetDryRun() {
		o.LogInfo(fmt.Sprintf("OPCUAIf [Simulated Mode] initialized for %s (%d monitored nodes)\n", endpointURL, len(orderCopy)))
		return
	}

	o.LogInfo(fmt.Sprintf("OPCUAIf init %s\n", endpointURL))
	o.clientMu.Lock()
	o.ctx, o.cancel = context.WithCancel(context.Background())
	ctx := o.ctx
	o.clientMu.Unlock()

	if reuseSession {
		o.registerAllSubscriptions(ctx, existingClient)
		return
	}

	readyCh := make(chan struct{})
	go o.connectAndSubscribeLoop(ctx, endpointURL, readyCh)
	select {
	case <-readyCh:
	case <-time.After(250 * time.Millisecond):
	}
}

// collectProfileSubscriptions scans all touch areas ("center", "left", "right"),
// "knobs", and "buttons" in prof in deterministic key order, expands node ID
// templates using baseOptions, and records valid OPC UA node IDs found in
// element params ("nodeid", "blink") and states ("opcua", "blink").
func (o *OPCUAIf) collectProfileSubscriptions(baseOptions map[string]interface{}, prof *config.Profile) {
	if prof == nil {
		return
	}
	type fieldSpec struct {
		name string
		data map[string]interface{}
	}
	var fields []fieldSpec
	fields = append(fields,
		fieldSpec{name: "center", data: prof.Touch.Center},
		fieldSpec{name: "left", data: prof.Touch.Left},
		fieldSpec{name: "right", data: prof.Touch.Right},
		fieldSpec{name: "knobs", data: prof.Knobs},
		fieldSpec{name: "buttons", data: prof.Buttons},
	)

	for _, f := range fields {
		if f.data == nil {
			continue
		}
		buttonNames := config.OrderedKeys(f.data)
		for i, buttonID := range buttonNames {
			elem, ok := f.data[buttonID].(map[string]interface{})
			if !ok || elem == nil {
				continue
			}
			localOptions := config.CloneMap(baseOptions)
			localOptions["key"] = buttonID

			if params, ok := elem["params"].(map[string]interface{}); ok && params != nil {
				for _, attr := range []string{"nodeid", "blink"} {
					if rawVal, exists := params[attr]; exists && rawVal != nil {
						nodeID := o.FormatString(config.ToString(rawVal), localOptions, nil)
						if o.IsNodeID(nodeID) {
							o.addDesiredSubscription(nodeID, i, buttonID, attr, f.name)
						}
					}
				}
			}

			if states, ok := elem["states"].(map[string]interface{}); ok && states != nil {
				for _, sk := range config.OrderedKeys(states) {
					state, ok := states[sk].(map[string]interface{})
					if !ok || state == nil {
						continue
					}
					for _, attr := range []string{"opcua", "blink"} {
						if rawVal, exists := state[attr]; exists && rawVal != nil {
							nodeID := o.FormatString(config.ToString(rawVal), localOptions, nil)
							if o.IsNodeID(nodeID) {
								o.addDesiredSubscription(nodeID, i, buttonID, attr, f.name)
							}
						}
					}
				}
			}
		}
	}
}

// addDesiredSubscription registers a SubscriptionMapping for nodeID under
// subMu, preserving insertion order in desiredOrder and deduplicating identical
// (field, buttonName, attribute) tuples.
func (o *OPCUAIf) addDesiredSubscription(nodeID string, buttonIndex int, buttonName, attribute, field string) {
	o.subMu.Lock()
	defer o.subMu.Unlock()
	list, exists := o.desiredSubscriptions[nodeID]
	if !exists {
		o.desiredOrder = append(o.desiredOrder, nodeID)
	}
	for _, existing := range list {
		if existing.Field == field && existing.ButtonName == buttonName && existing.Attribute == attribute {
			return
		}
	}
	o.desiredSubscriptions[nodeID] = append(list, SubscriptionMapping{
		Field:      field,
		ButtonID:   buttonIndex,
		ButtonName: buttonName,
		Attribute:  attribute,
	})
}

// connectAndSubscribeLoop discovers endpoints at endpointURL, establishes an
// anonymous OPC UA client session with auto-reconnect enabled, and registers all
// desired subscriptions. On connection failure, it closes readyCh on the first
// attempt and retries every 5 seconds until ctx is cancelled or connection
// succeeds.
func (o *OPCUAIf) connectAndSubscribeLoop(ctx context.Context, endpointURL string, readyCh chan struct{}) {
	var readyOnce sync.Once
	notifyReady := func() {
		if readyCh != nil {
			readyOnce.Do(func() {
				close(readyCh)
			})
		}
	}
	defer notifyReady()

	retry := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		opts := []opcua.Option{
			opcua.SecurityMode(ua.MessageSecurityModeNone),
			opcua.SecurityPolicy(ua.SecurityPolicyURINone),
			opcua.AutoReconnect(true),
			opcua.ReconnectInterval(5 * time.Second),
			opcua.AuthAnonymous(),
		}

		epCtx, epCancel := context.WithTimeout(ctx, 3*time.Second)
		eps, epErr := opcua.GetEndpoints(epCtx, endpointURL)
		epCancel()
		if epErr == nil && len(eps) > 0 {
			ep, selErr := opcua.SelectEndpoint(eps, ua.SecurityPolicyURINone, ua.MessageSecurityModeNone)
			if selErr != nil || ep == nil {
				ep = eps[0]
			}
			opts = []opcua.Option{
				opcua.SecurityFromEndpoint(ep, ua.UserTokenTypeAnonymous),
				opcua.AuthAnonymous(),
				opcua.AutoReconnect(true),
				opcua.ReconnectInterval(5 * time.Second),
			}
			for _, tok := range ep.UserIdentityTokens {
				if tok != nil && tok.TokenType == ua.UserTokenTypeAnonymous && tok.PolicyID != "" {
					opts = append(opts, opcua.AuthPolicyID(tok.PolicyID))
					break
				}
			}
		}

		client, err := opcua.NewClient(endpointURL, opts...)
		if err != nil {
			o.LogError(fmt.Sprintf("OPCUAIf: client creation error: %v\n", err))
			return
		}

		connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = client.Connect(connCtx)
		cancel()
		if err != nil {
			notifyReady()
			retry++
			if retry == 1 || retry%10 == 0 {
				o.LogInfo(fmt.Sprintf("OPCUAIf: Try Reconnection (attempt %d), next attempt in 5000ms to %s (%v)\n", retry, endpointURL, err))
			}
			_ = client.Close(context.Background())
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		o.clientMu.Lock()
		o.client = client
		o.connected.Store(true)
		o.clientMu.Unlock()

		o.LogInfo("OPCUAIf: session created\n")
		o.registerAllSubscriptions(ctx, client)
		notifyReady()
		return
	}
}

// registerAllSubscriptions creates an OPC UA subscription on client using the
// configured publishing interval (defaulting to 250ms), monitors all valid node
// IDs in desiredOrder, and spawns a background goroutine bound to ctx that
// processes DataChangeNotification messages, caches node data types and values,
// and dispatches updates to OnMonitoredItemChanged.
func (o *OPCUAIf) registerAllSubscriptions(ctx context.Context, client *opcua.Client) {
	notifyCh := make(chan *opcua.PublishNotificationData, 256)
	o.subMu.RLock()
	pubInt := o.publishingInterval
	if pubInt <= 0 {
		pubInt = 250
	}
	orderCopy := append([]string(nil), o.desiredOrder...)
	o.subMu.RUnlock()

	sub, err := client.Subscribe(ctx, &opcua.SubscriptionParameters{
		Interval:                   time.Duration(pubInt) * time.Millisecond,
		LifetimeCount:              300,
		MaxKeepAliveCount:          20,
		MaxNotificationsPerPublish: 9000,
	}, notifyCh)
	if err != nil {
		o.LogError(fmt.Sprintf("OPCUAIf: createSubscription error: %v\n", err))
		return
	}

	o.clientMu.Lock()
	o.sub = sub
	o.clientMu.Unlock()

	var reqs []*ua.MonitoredItemCreateRequest
	o.subMu.Lock()
	for _, nodeID := range orderCopy {
		parsed, err := ua.ParseNodeID(nodeID)
		if err != nil {
			continue
		}
		hdl := o.nextHdl.Add(1)
		o.nodeByHandle[hdl] = nodeID
		req := opcua.NewMonitoredItemCreateRequestWithDefaults(parsed, ua.AttributeIDValue, hdl)
		reqs = append(reqs, req)
	}
	o.subMu.Unlock()

	if len(reqs) > 0 {
		if _, err := sub.Monitor(ctx, ua.TimestampsToReturnBoth, reqs...); err != nil {
			o.LogError(fmt.Sprintf("OPCUAIf: Monitor error: %v\n", err))
		}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-notifyCh:
				if !ok || msg == nil || msg.Error != nil {
					continue
				}
				dcn, ok := msg.Value.(*ua.DataChangeNotification)
				if !ok {
					continue
				}
				for _, item := range dcn.MonitoredItems {
					if item == nil || item.Value == nil || item.Value.Status != ua.StatusOK || item.Value.Value == nil {
						continue
					}
					val := item.Value.Value.Value()
					dt := item.Value.Value.Type()
					o.subMu.Lock()
					nodeID := o.nodeByHandle[item.ClientHandle]
					if nodeID != "" {
						if dt != ua.TypeIDNull {
							o.types[nodeID] = dt
						}
						o.simulatedValues[nodeID] = val
					}
					mappings := append([]SubscriptionMapping(nil), o.desiredSubscriptions[nodeID]...)
					o.subMu.Unlock()

					if nodeID == "" || len(mappings) == 0 {
						continue
					}
					var list []string
					for _, m := range mappings {
						target := m.ButtonName
						if target == "" {
							target = strconv.Itoa(m.ButtonID)
						}
						lbl := fmt.Sprintf("%s:%s", target, m.Attribute)
						if m.Field != "" {
							lbl = fmt.Sprintf("%s.%s:%s", m.Field, target, m.Attribute)
						}
						list = append(list, lbl)
						o.emitMonitoredChanged(target, m.Attribute, nodeID, val, m.Field)
					}
					o.LogInfo(fmt.Sprintf("OPCUAIf: monitored item %s changed value: %v - push to buttons: %s\n", nodeID, val, strings.Join(list, ", ")))
				}
			}
		}
	}()
}

// Disconnect cancels the active subscription context, cancels any active OPC UA
// subscription, and closes the OPC UA client connection under clientMu (each
// with a 2-second timeout), marking the interface disconnected. It always
// returns nil.
func (o *OPCUAIf) Disconnect() error {
	o.clientMu.Lock()
	defer o.clientMu.Unlock()
	o.connected.Store(false)
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
	if o.sub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = o.sub.Cancel(ctx)
		cancel()
		o.sub = nil
	}
	if o.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = o.client.Close(ctx)
		cancel()
		o.client = nil
		o.LogInfo("OPCUAIf: Disconnected\n")
	}
	return nil
}

// Stop disconnects the OPC UA client, logs shutdown, and returns nil.
func (o *OPCUAIf) Stop() error {
	_ = o.Disconnect()
	o.LogInfo("OPCUAIf Stopped\n")
	return nil
}

// Check validates that options contains all mandatory BaseIf parameters as well
// as the OPC UA-specific "endpointurl", "nodeid", and "value" keys. It returns
// 0 on success, the BaseIf error code on base validation failure, or -11, -12,
// or -13 when "endpointurl", "nodeid", or "value" is missing respectively.
func (o *OPCUAIf) Check(options map[string]interface{}) int {
	res := o.BaseIf.Check(options)
	if res < 0 {
		o.LogError("OPCUAIf: mandatory parameter missing\n")
		return res
	}
	if _, ok := options["endpointurl"]; !ok {
		o.LogError("OPCUAIf: mandatory parameter endpointurl missing\n")
		return -11
	}
	if _, ok := options["nodeid"]; !ok {
		o.LogError("OPCUAIf: mandatory parameter nodeid missing\n")
		return -12
	}
	if _, ok := options["value"]; !ok {
		o.LogError("OPCUAIf: mandatory parameter value missing\n")
		return -13
	}
	return 0
}

// Call formats opcuaNode and options["value"] (when a string) and writes the
// resulting value to the target OPC UA node, or records a simulated write when
// dry-run mode is enabled.
//
// In live mode, Call requires an active client connection; if the node's OPC UA
// data type is not yet cached in o.types, it performs an initial AttributeIDValue
// read (with a 5-second request timeout) to infer and cache the target TypeID
// before converting the value via convertVariant and issuing a WriteRequest.
//
// Call emits an ActivityEntry for validation errors, simulated writes,
// disconnected clients, data-type lookup failures, and write attempts. It
// returns ("waiting", nil) after simulated writes or executed write requests,
// (false, err) on node ID parsing or variant conversion errors, and (false, nil)
// on validation, connection, or data-type resolution failures.
func (o *OPCUAIf) Call(opcuaNode string, options map[string]interface{}) (interface{}, error) {
	res := o.Check(options)
	dryRun := o.GetDryRun()
	if res < 0 {
		o.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "opcua",
			Status:    "error",
			Simulated: dryRun,
			Raw:       opcuaNode,
			Formatted: "",
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("OPCUA Check failed with code %d", res),
		})
		return false, nil
	}

	nodeID, ok := o.BaseCall(opcuaNode, options, nil)
	if !ok {
		return false, nil
	}
	valRaw := options["value"]
	if valStr, isStr := valRaw.(string); isStr {
		valRaw = o.FormatString(valStr, options, nil)
	}

	if dryRun {
		o.LogInfo(fmt.Sprintf("OPCUAIf [Simulated Write]: %s => %v\n", nodeID, valRaw))
		o.subMu.Lock()
		o.simulatedValues[nodeID] = valRaw
		o.subMu.Unlock()
		o.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "opcua",
			Status:    "ok",
			Simulated: true,
			Raw:       opcuaNode,
			Formatted: fmt.Sprintf("%s <= %v", nodeID, valRaw),
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("[Simulated Write] %s <= %v", nodeID, valRaw),
		})
		return "waiting", nil
	}

	if nodeID == "" || !o.connected.Load() {
		o.LogError(fmt.Sprintf("OPCUAIf: Cannot call %s - client not connected\n", nodeID))
		o.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "opcua",
			Status:    "error",
			Simulated: false,
			Raw:       opcuaNode,
			Formatted: nodeID,
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("Cannot call %s - client not connected", nodeID),
		})
		return false, nil
	}

	o.clientMu.Lock()
	client := o.client
	o.clientMu.Unlock()
	if client == nil {
		return false, nil
	}

	parsedNode, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	o.subMu.RLock()
	knownType := o.types[nodeID]
	o.subMu.RUnlock()

	if knownType == ua.TypeIDNull {
		rreq := &ua.ReadRequest{
			MaxAge: 0,
			NodesToRead: []*ua.ReadValueID{
				{NodeID: parsedNode, AttributeID: ua.AttributeIDValue},
			},
			TimestampsToReturn: ua.TimestampsToReturnBoth,
		}
		rres, rerr := client.Read(ctx, rreq)
		if rerr == nil && len(rres.Results) > 0 && rres.Results[0].Status == ua.StatusOK && rres.Results[0].Value != nil {
			knownType = rres.Results[0].Value.Type()
			o.subMu.Lock()
			o.types[nodeID] = knownType
			o.subMu.Unlock()
		} else {
			o.LogError(fmt.Sprintf("OPCUAIf: Could not determine DataType for %s\n", nodeID))
			o.EmitActivity(ActivityEntry{
				Timestamp: time.Now().UnixMilli(),
				Interface: "opcua",
				Status:    "error",
				Simulated: false,
				Raw:       opcuaNode,
				Formatted: nodeID,
				ButtonID:  options["id"],
				State:     config.ToString(options["state"]),
				Event:     config.ToString(options["event"]),
				Message:   fmt.Sprintf("Could not determine DataType for %s", nodeID),
			})
			return false, nil
		}
	}

	variant, convertedVal, err := convertVariant(valRaw, knownType)
	if err != nil {
		return false, err
	}

	wreq := &ua.WriteRequest{
		NodesToWrite: []*ua.WriteValue{
			{
				NodeID:      parsedNode,
				AttributeID: ua.AttributeIDValue,
				Value: &ua.DataValue{
					EncodingMask: ua.DataValueValue,
					Value:        variant,
				},
			},
		},
	}
	wres, err := client.Write(ctx, wreq)
	writeOk := err == nil && len(wres.Results) > 0 && wres.Results[0] == ua.StatusOK
	if writeOk {
		o.LogInfo(fmt.Sprintf("OPCUAIf: wrote %s => %v\n", nodeID, convertedVal))
	} else {
		o.LogError(fmt.Sprintf("OPCUAIf: writing not OK %s => %v\n", nodeID, convertedVal))
	}

	status := "error"
	msg := fmt.Sprintf("Write failed for %s", nodeID)
	if writeOk {
		status = "ok"
		msg = fmt.Sprintf("Wrote %s <= %v", nodeID, convertedVal)
	}
	o.EmitActivity(ActivityEntry{
		Timestamp: time.Now().UnixMilli(),
		Interface: "opcua",
		Status:    status,
		Simulated: false,
		Raw:       opcuaNode,
		Formatted: fmt.Sprintf("%s <= %v", nodeID, convertedVal),
		ButtonID:  options["id"],
		State:     config.ToString(options["state"]),
		Event:     config.ToString(options["event"]),
		Message:   msg,
	})
	return "waiting", nil
}

// convertVariant coerces value to the Go type corresponding to targetType
// (supporting Boolean, SByte, Byte, Int16..Uint64, Float, Double, and String,
// with string fallback for unrecognized types) and wraps it in a *ua.Variant.
// It returns the constructed variant, the converted Go value, and any variant
// construction error.
func convertVariant(value interface{}, targetType ua.TypeID) (*ua.Variant, interface{}, error) {
	valStr := config.ToString(value)
	switch targetType {
	case ua.TypeIDBoolean:
		var b bool
		switch v := value.(type) {
		case bool:
			b = v
		case int, int64, float64:
			b = config.ToIntDefault(v, 0) == 1
		default:
			lower := strings.ToLower(strings.TrimSpace(valStr))
			b = lower == "true" || lower == "on" || lower == "1"
		}
		v, err := ua.NewVariant(b)
		return v, b, err
	case ua.TypeIDSByte:
		n := int8(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDByte:
		n := uint8(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDInt16:
		n := int16(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDUint16:
		n := uint16(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDInt32:
		n := int32(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDUint32:
		n := uint32(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDInt64:
		n := int64(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDUint64:
		n := uint64(config.ToIntDefault(value, 0))
		v, err := ua.NewVariant(n)
		return v, n, err
	case ua.TypeIDFloat:
		f := float32(config.ToFloatDefault(value, 0))
		v, err := ua.NewVariant(f)
		return v, f, err
	case ua.TypeIDDouble:
		f := config.ToFloatDefault(value, 0)
		v, err := ua.NewVariant(f)
		return v, f, err
	case ua.TypeIDString:
		v, err := ua.NewVariant(valStr)
		return v, valStr, err
	default:
		_ = sort.Strings
		v, err := ua.NewVariant(valStr)
		return v, valStr, err
	}
}
