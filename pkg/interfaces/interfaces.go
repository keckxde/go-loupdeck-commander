package interfaces

import (
	"fmt"
	"os"
	"sync"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
)

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

type ActivityEmitter func(entry ActivityEntry)

type ProfileEvent struct {
	Type  string // "profileChanged", "brightnessChanged", "vibrate"
	Value interface{}
}

type MonitoredItemChangedFunc func(buttonID string, attribute string, nodeID string, value interface{}, field string)

type BaseIf struct {
	Mu              sync.RWMutex
	DryRun          bool
	ActivityEmitter ActivityEmitter
	Options         map[string]interface{}
}

func (b *BaseIf) SetDryRun(enabled bool) {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	b.DryRun = enabled
}

func (b *BaseIf) GetDryRun() bool {
	b.Mu.RLock()
	defer b.Mu.RUnlock()
	return b.DryRun
}

func (b *BaseIf) SetActivityEmitter(fn ActivityEmitter) {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	b.ActivityEmitter = fn
}

func (b *BaseIf) EmitActivity(entry ActivityEntry) {
	b.Mu.RLock()
	fn := b.ActivityEmitter
	b.Mu.RUnlock()
	if fn != nil {
		fn(entry)
	}
}

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

func (b *BaseIf) FormatString(cmd string, options map[string]interface{}, transform func(string, string) string) string {
	return config.FormatTemplateTransform(cmd, options, transform)
}

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

func (b *BaseIf) LogError(msg string) {
	_, _ = os.Stderr.WriteString(msg)
}

func (b *BaseIf) LogInfo(msg string) {
	_, _ = os.Stdout.WriteString(msg)
}

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

type Manager struct {
	Shell          *SHELLif
	HTTP           *HTTPif
	OPCUA          *OPCUAIf
	ProfileEvents  chan ProfileEvent
	OnProfileEvent func(evt ProfileEvent)
	simMu          sync.RWMutex
	simulation     bool
}

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

func (m *Manager) GetSimulationMode() bool {
	if m == nil {
		return false
	}
	m.simMu.RLock()
	defer m.simMu.RUnlock()
	return m.simulation
}

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
