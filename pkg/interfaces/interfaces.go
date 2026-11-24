// Package interfaces provides action execution backends (Shell, HTTP, and OPC UA)
// along with shared base functionality, activity reporting, simulation/dry-run
// support, and lifecycle management via Manager.
package interfaces

import (
	"fmt"
	"os"
	"sync"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
)

// ActivityEntry represents a single recorded execution or subscription event
// emitted by an interface backend for logging, UI monitoring, or diagnostics.
type ActivityEntry struct {
	Timestamp int64       `json:"timestamp"`
	Interface string      `json:"interface"`
	Status    string      `json:"status"`
	Simulated bool        `json:"simulated"`
	Raw       string      `json:"raw"`
	Formatted string      `json:"formatted"`
	ButtonID  interface{} `json:"buttonId,omitempty"`
	State     string      `json:"state,omitempty"`
	Event     string      `json:"event,omitempty"`
	Message   string      `json:"message"`
}

// ActivityEmitter is a callback invoked by interfaces whenever an ActivityEntry
// is produced.
type ActivityEmitter func(entry ActivityEntry)

// ProfileEvent represents a runtime event affecting the active profile or
// hardware state, such as "profileChanged", "brightnessChanged", or "vibrate".
type ProfileEvent struct {
	Type  string // "profileChanged", "brightnessChanged", "vibrate"
	Value interface{}
}

// MonitoredItemChangedFunc is a callback invoked when an OPC UA monitored item
// (or simulated node value) updates, mapping the change to a target button,
// attribute, node ID, new value, and layout field.
type MonitoredItemChangedFunc func(buttonID string, attribute string, nodeID string, value interface{}, field string)

// BaseIf provides common state, synchronization, template formatting, parameter
// validation, logging, and activity emission shared by all interface backends.
// Its fields are guarded by Mu for concurrent access across goroutines.
type BaseIf struct {
	Mu              sync.RWMutex
	DryRun          bool
	ActivityEmitter ActivityEmitter
	Options         map[string]interface{}
}

// SetDryRun enables or disables simulation (dry-run) mode on the interface.
// It acquires BaseIf.Mu for writing.
func (b *BaseIf) SetDryRun(enabled bool) {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	b.DryRun = enabled
}

// GetDryRun reports whether simulation (dry-run) mode is currently enabled.
// It is safe for concurrent use via BaseIf.Mu read locking.
func (b *BaseIf) GetDryRun() bool {
	b.Mu.RLock()
	defer b.Mu.RUnlock()
	return b.DryRun
}

// SetActivityEmitter registers the callback function fn to receive emitted
// ActivityEntry events. Passing nil disables activity emission.
func (b *BaseIf) SetActivityEmitter(fn ActivityEmitter) {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	b.ActivityEmitter = fn
}

// EmitActivity dispatches entry to the registered ActivityEmitter callback, if
// one is set. The callback is invoked outside BaseIf.Mu to avoid lock contention
// or reentrancy deadlocks.
func (b *BaseIf) EmitActivity(entry ActivityEntry) {
	b.Mu.RLock()
	fn := b.ActivityEmitter
	b.Mu.RUnlock()
	if fn != nil {
		fn(entry)
	}
}

// Check validates that options is non-nil and contains all mandatory base keys
// ("id", "key", "state", "min", "max", "color", and "image"). It returns 0 if
// valid, or a negative error code (-1 through -7) identifying the missing key.
func (b *BaseIf) Check(options map[string]interface{}) int {
	if options == nil {
		return -1
	}
	if _, ok := options["id"]; !ok {
		return -1
	}
	if _, ok := options["key"]; !ok {
		return -2
	}
	if _, ok := options["state"]; !ok {
		return -3
	}
	if _, ok := options["min"]; !ok {
		return -4
	}
	if _, ok := options["max"]; !ok {
		return -5
	}
	if _, ok := options["color"]; !ok {
		return -6
	}
	if _, ok := options["image"]; !ok {
		return -7
	}
	return 0
}

// FormatString expands template placeholders in cmd using options and an
// optional value transform callback via config.FormatTemplateTransform,
// returning the formatted string.
func (b *BaseIf) FormatString(cmd string, options map[string]interface{}, transform func(string, string) string) string {
	return config.FormatTemplateTransform(cmd, options, transform)
}

// BaseCall validates options via Check, caches options on BaseIf.Options under
// Mu, and formats cmd using FormatString with the optional transform function.
// It returns the formatted string and true on success, or ("", false) if
// mandatory options are missing.
func (b *BaseIf) BaseCall(cmd string, options map[string]interface{}, transform func(string, string) string) (string, bool) {
	res := b.Check(options)
	if res < 0 {
		b.LogError(fmt.Sprintf("Missing essential options in dictionary => Quitting (%d)\n", res))
		return "", false
	}
	b.Mu.Lock()
	b.Options = options
	b.Mu.Unlock()
	formatted := b.FormatString(cmd, options, transform)
	return formatted, true
}

// LogError writes msg directly to standard error (os.Stderr).
func (b *BaseIf) LogError(msg string) {
	_, _ = os.Stderr.WriteString(msg)
}

// LogInfo writes msg directly to standard output (os.Stdout).
func (b *BaseIf) LogInfo(msg string) {
	_, _ = os.Stdout.WriteString(msg)
}

// LogDebug writes msg to standard output only when the most recently cached
// Options map has "verbose" set to true. It reads BaseIf.Options under Mu.
func (b *BaseIf) LogDebug(msg string) {
	b.Mu.RLock()
	verbose := false
	if b.Options != nil {
		if v, ok := b.Options["verbose"].(bool); ok && v {
			verbose = true
		}
	}
	b.Mu.RUnlock()
	if verbose {
		_, _ = os.Stdout.WriteString(msg)
	}
}

// Manager coordinates the lifecycle, simulation mode, activity emission, and
// profile event dispatching across the Shell, HTTP, and OPC UA interface
// backends.
type Manager struct {
	Shell          *SHELLif
	HTTP           *HTTPif
	OPCUA          *OPCUAIf
	ProfileEvents  chan ProfileEvent
	OnProfileEvent func(evt ProfileEvent)
	simMu          sync.RWMutex
	simulation     bool
}

// NewManager constructs a Manager with initialized Shell, HTTP, and OPC UA
// interfaces, configuring each with the initial simulate (dry-run) flag and
// activity emitter, and allocating a buffered ProfileEvents channel (capacity 128).
func NewManager(simulate bool, emitter ActivityEmitter) *Manager {
	mgr := &Manager{
		Shell:         NewSHELLif(),
		HTTP:          NewHTTPif(),
		OPCUA:         NewOPCUAIf(),
		ProfileEvents: make(chan ProfileEvent, 128),
		simulation:    simulate,
	}
	mgr.Shell.SetDryRun(simulate)
	mgr.HTTP.SetDryRun(simulate)
	mgr.OPCUA.SetDryRun(simulate)
	mgr.Shell.SetActivityEmitter(emitter)
	mgr.HTTP.SetActivityEmitter(emitter)
	mgr.OPCUA.SetActivityEmitter(emitter)
	return mgr
}

// InitializeProfile applies the current simulation mode to all interfaces and
// initializes OPC UA subscriptions and endpoint settings from prof. If m or
// prof is nil, it is a no-op.
func (m *Manager) InitializeProfile(prof *config.Profile) {
	if m == nil || prof == nil {
		return
	}
	sim := m.GetSimulationMode()
	m.Shell.SetDryRun(sim)
	m.HTTP.SetDryRun(sim)
	m.OPCUA.SetDryRun(sim)
	m.OPCUA.Init(prof.Parameters, prof)
}

// StopInterfaces gracefully stops all managed interface backends (OPC UA,
// HTTP, and Shell). It is safe to call on a nil Manager or with nil backends.
func (m *Manager) StopInterfaces() {
	if m == nil {
		return
	}
	if m.OPCUA != nil {
		_ = m.OPCUA.Stop()
	}
	if m.HTTP != nil {
		_ = m.HTTP.Stop()
	}
	if m.Shell != nil {
		_ = m.Shell.Stop()
	}
}

// SetSimulationMode updates the manager's simulation mode flag under simMu and
// propagates the dry-run setting to all non-nil interface backends. For OPC UA,
// toggling out of dry-run mode may trigger reconnection when an endpoint is
// configured.
func (m *Manager) SetSimulationMode(enabled bool) {
	if m == nil {
		return
	}
	m.simMu.Lock()
	m.simulation = enabled
	m.simMu.Unlock()
	if m.Shell != nil {
		m.Shell.SetDryRun(enabled)
	}
	if m.HTTP != nil {
		m.HTTP.SetDryRun(enabled)
	}
	if m.OPCUA != nil {
		m.OPCUA.SetDryRun(enabled)
	}
}

// GetSimulationMode reports whether simulation mode is currently enabled on m.
// It returns false if m is nil and is safe for concurrent use.
func (m *Manager) GetSimulationMode() bool {
	if m == nil {
		return false
	}
	m.simMu.RLock()
	defer m.simMu.RUnlock()
	return m.simulation
}

// EmitProfileEvent delivers evt to OnProfileEvent synchronously if configured,
// or performs a non-blocking send to the buffered ProfileEvents channel (dropping
// the event if the buffer is full). If m is nil, it is a no-op.
func (m *Manager) EmitProfileEvent(evt ProfileEvent) {
	if m == nil {
		return
	}
	if m.OnProfileEvent != nil {
		m.OnProfileEvent(evt)
		return
	}
	select {
	case m.ProfileEvents <- evt:
	default:
	}
}
