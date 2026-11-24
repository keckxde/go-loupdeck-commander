// Package commander implements the Loupedeck device orchestration layer,
// managing button fields, multi-state buttons, profile switching, hardware/emulated/hybrid
// device lifecycles, display rendering loops, and external interface dispatching.
package commander

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/device"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/interfaces"
)

// LayoutCompatibility describes how the configured primary device and grid
// layout compare to the currently connected Loupedeck device, including
// effective grid dimensions and any layout/display mismatch warnings.
type LayoutCompatibility struct {
	MatchesPrimaryDevice bool     `json:"matchesPrimaryDevice"`
	PrimaryDevice        string   `json:"primaryDevice"`
	PrimaryDeviceID      string   `json:"primaryDeviceId"`
	ConnectedDevice      string   `json:"connectedDevice"`
	ConnectedDeviceID    *string  `json:"connectedDeviceId"`
	PrimaryColumns       int      `json:"primaryColumns"`
	PrimaryRows          int      `json:"primaryRows"`
	DeviceColumns        int      `json:"deviceColumns"`
	DeviceRows           int      `json:"deviceRows"`
	EffectiveColumns     int      `json:"effectiveColumns"`
	EffectiveRows        int      `json:"effectiveRows"`
	Warnings             []string `json:"warnings"`
}

// HandlerOptions configures device selection, emulation/hybrid modes,
// reconnection behavior, dry-run execution, and activity reporting for
// NewBaseLoupeDeckHandler.
type HandlerOptions struct {
	Device             device.Device
	HardwareDevice     device.Device
	Emulate            bool
	Hybrid             bool
	Model              string
	DryRun             bool
	AllowInvalidConfig bool
	ExitOnStop         bool
	AutoReconnect      *bool
	ReconnectDelayMs   int
	BaseDir            string
	ActivityEmitter    interfaces.ActivityEmitter
}

// BaseLoupeDeckHandler coordinates device connections (hardware, emulated, or
// hybrid), configuration loading, profile activation, touchscreen/LED rendering,
// input routing, and external interface integrations (Shell, HTTP, OPC UA).
// Concurrent access is guarded by Mu, activateMu, and updateMu.
type BaseLoupeDeckHandler struct {
	Mu                  sync.RWMutex
	Device              device.Device
	EmulatedDevice      *device.EmulatedLoupedeckDevice
	HybridDevice        *device.HybridLoupedeckDevice
	HardwareDevice      device.Device
	Emulated            bool
	Hybrid              bool
	HardwareConnected   bool
	HardwareAddress     string
	ConfigFile          string
	BaseDir             string
	AppConfig           *config.ApplicationConfig
	CurrentProfile      int
	Brightness          float64
	Screens             map[string]*ButtonField
	Buttons             *ButtonField
	Knobs               *ButtonField
	LayoutCompatibility LayoutCompatibility
	Counter             int
	Stopping            bool
	ExitOnStop          bool
	AutoReconnect       bool
	ReconnectDelayMs    int
	Ifaces              *interfaces.Manager

	activateMu                 sync.Mutex
	updateMu                   sync.Mutex
	updatingScreens            bool
	postSwitchRefreshRemaining int
	postSwitchTickCounter      int
	reconnecting               bool
	stopCh                     chan struct{}
	tickerStopCh               chan struct{}
	OnStateChange              func()
}

// NewBaseLoupeDeckHandler creates a BaseLoupeDeckHandler, loads configFile
// (returning an error if loading fails unless opts.AllowInvalidConfig is true),
// configures the target device (hybrid, provided, or emulated), and wires OPC UA
// monitored item and profile event callbacks.
func NewBaseLoupeDeckHandler(configFile string, opts HandlerOptions) (*BaseLoupeDeckHandler, error) {
	fmt.Printf("INIT with config %s\n", configFile)

	autoReconnect := !(opts.Device != nil || opts.Emulate) || (opts.Hybrid && opts.HardwareDevice == nil)
	if opts.AutoReconnect != nil {
		autoReconnect = *opts.AutoReconnect
	}
	reconnectDelay := 3000
	if opts.ReconnectDelayMs > 0 {
		reconnectDelay = opts.ReconnectDelayMs
	}

	h := &BaseLoupeDeckHandler{
		ConfigFile:       configFile,
		BaseDir:          opts.BaseDir,
		Brightness:       1.0,
		Screens:          make(map[string]*ButtonField),
		ExitOnStop:       opts.ExitOnStop,
		AutoReconnect:    autoReconnect,
		ReconnectDelayMs: reconnectDelay,
		Ifaces:           interfaces.NewManager(opts.DryRun, opts.ActivityEmitter),
		stopCh:           make(chan struct{}),
		LayoutCompatibility: LayoutCompatibility{
			MatchesPrimaryDevice: true,
			PrimaryDevice:        "Loupedeck Live",
			PrimaryDeviceID:      "loupedeck-live",
			ConnectedDevice:      "Disconnected",
			PrimaryColumns:       4,
			PrimaryRows:          3,
			DeviceColumns:        4,
			DeviceRows:           3,
			EffectiveColumns:     4,
			EffectiveRows:        3,
			Warnings:             []string{},
		},
	}

	ok := h.LoadConfig(configFile)
	if !ok && !opts.AllowInvalidConfig {
		return nil, fmt.Errorf("failed to load configuration %s", configFile)
	}

	if opts.Hybrid {
		defaultModel := opts.Model
		if defaultModel == "" && h.AppConfig != nil && h.AppConfig.Layout.DeviceID != "" {
			defaultModel = h.AppConfig.Layout.DeviceID
		}
		if defaultModel == "" {
			defaultModel = "live"
		}
		var emu *device.EmulatedLoupedeckDevice
		if e, ok := opts.Device.(*device.EmulatedLoupedeckDevice); ok {
			emu = e
		} else if hyb, ok := opts.Device.(*device.HybridLoupedeckDevice); ok && hyb.Emulated != nil {
			emu = hyb.Emulated
		} else {
			emu = device.NewEmulatedLoupedeckDevice(device.EmulatedDeviceOptions{Model: defaultModel})
		}
		hyb := device.NewHybridLoupedeckDevice(emu, opts.HardwareDevice)
		h.Hybrid = true
		h.HybridDevice = hyb
		h.EmulatedDevice = emu
		h.HardwareDevice = opts.HardwareDevice
		h.Device = hyb
		h.Emulated = true
	} else if opts.Device != nil {
		h.Device = opts.Device
		if emu, isEmu := opts.Device.(*device.EmulatedLoupedeckDevice); isEmu {
			h.EmulatedDevice = emu
		}
		h.Emulated = opts.Emulate || h.EmulatedDevice != nil
	} else if opts.Emulate {
		defaultModel := opts.Model
		if defaultModel == "" && h.AppConfig != nil && h.AppConfig.Layout.DeviceID != "" {
			defaultModel = h.AppConfig.Layout.DeviceID
		}
		if defaultModel == "" {
			defaultModel = "live"
		}
		emu := device.NewEmulatedLoupedeckDevice(device.EmulatedDeviceOptions{Model: defaultModel})
		h.Device = emu
		h.EmulatedDevice = emu
		h.Emulated = true
	}

	h.Ifaces.OPCUA.SetOnMonitoredItemChanged(func(buttonID, attribute, nodeID string, value interface{}, field string) {
		h.ButtonStateChanged(buttonID, attribute, nodeID, value, field)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	})

	h.Ifaces.OnProfileEvent = func(evt interfaces.ProfileEvent) {
		h.handleProfileEvent(evt)
	}

	return h, nil
}

// handleProfileEvent dispatches internal profile events emitted by buttons or
// interfaces: "profileChanged" (switching or resyncing profiles),
// "brightnessChanged" (normalizing percentage values > 1.0), and "vibrate".
func (h *BaseLoupeDeckHandler) handleProfileEvent(evt interfaces.ProfileEvent) {
	switch evt.Type {
	case "profileChanged":
		targetID := config.ToIntDefault(evt.Value, 0)
		h.Mu.RLock()
		curProf := h.CurrentProfile
		hasCenter := h.Screens["center"] != nil
		h.Mu.RUnlock()
		if targetID == curProf && hasCenter {
			h.syncProfileButtonsState()
			if h.OnStateChange != nil {
				h.OnStateChange()
			}
			return
		}
		_ = h.ActivateProfile(targetID)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	case "brightnessChanged":
		bVal := config.ToFloatDefault(evt.Value, 1.0)
		if bVal > 1.0 {
			bVal = bVal / 100.0
		}
		_ = h.ChangeBrightness(bVal)
	case "vibrate":
		pat := parseVibratePattern(evt.Value)
		_ = h.Vibrate(pat)
	}
}

// parseVibratePattern converts val (byte, numeric, named device.HAPTIC key,
// hex "0x..." string, or decimal string) into a Loupedeck haptic pattern byte,
// defaulting to device.HAPTIC["SHORT"].
func parseVibratePattern(val interface{}) byte {
	switch v := val.(type) {
	case byte:
		return v
	case int:
		return byte(v)
	case float64:
		return byte(v)
	case string:
		s := strings.TrimSpace(v)
		if code, ok := device.HAPTIC[strings.ToUpper(s)]; ok {
			return code
		}
		if strings.HasPrefix(strings.ToLower(s), "0x") {
			if n, err := strconv.ParseUint(s[2:], 16, 8); err == nil {
				return byte(n)
			}
		}
		if n, err := strconv.ParseUint(s, 10, 8); err == nil {
			return byte(n)
		}
	}
	return device.HAPTIC["SHORT"]
}

// LoadConfig reads and parses an ApplicationConfig from fileName using
// h.BaseDir, stores it in h.AppConfig, and returns whether loading succeeded.
func (h *BaseLoupeDeckHandler) LoadConfig(fileName string) bool {
	fmt.Printf("Loading Config File %s\n", fileName)
	appCfg := config.NewApplicationConfig()
	appCfg.BaseDir = h.BaseDir
	ok := appCfg.LoadFromFile(fileName)
	h.Mu.Lock()
	h.AppConfig = appCfg
	h.Mu.Unlock()
	return ok
}

// Start clears the Stopping flag and initiates device connections according to
// the configured mode: hybrid (connecting both emulated and hardware devices, or
// starting auto-discovery if no hardware device was pre-supplied), emulated,
// pre-supplied Device, or background hardware discovery via connectToDeviceLoop.
func (h *BaseLoupeDeckHandler) Start() error {
	h.Mu.Lock()
	h.Stopping = false
	dev := h.Device
	emulated := h.Emulated
	hybrid := h.Hybrid
	hybDev := h.HybridDevice
	emuDev := h.EmulatedDevice
	hwDev := h.HardwareDevice
	h.Mu.Unlock()

	if hybrid && hybDev != nil {
		h.attachEmulatedListeners(emuDev)
		_ = emuDev.Connect()
		if hwDev != nil {
			if hw, ok := hwDev.(*device.HardwareLoupedeckDevice); ok {
				h.attachHardwareListeners(hw)
			}
			err := hwDev.Connect()
			if err == nil {
				h.Mu.Lock()
				h.HardwareConnected = true
				addr := h.HardwareAddress
				h.Mu.Unlock()
				if _, ok := hwDev.(*device.HardwareLoupedeckDevice); !ok && addr == "" {
					h.OnConnected("hardware://custom")
				}
			}
			return err
		}
		go h.connectToDeviceLoop()
		return nil
	}

	if emulated && h.EmulatedDevice != nil {
		h.attachEmulatedListeners(h.EmulatedDevice)
		_ = h.EmulatedDevice.Connect()
		return nil
	}

	if dev != nil {
		if hw, ok := dev.(*device.HardwareLoupedeckDevice); ok {
			h.attachHardwareListeners(hw)
		}
		return dev.Connect()
	}

	go h.connectToDeviceLoop()
	return nil
}

// attachEmulatedListeners binds connection and input callbacks on emu to the
// corresponding handler methods.
func (h *BaseLoupeDeckHandler) attachEmulatedListeners(emu *device.EmulatedLoupedeckDevice) {
	emu.OnConnect = func(address string) {
		h.OnConnected(address)
	}
	emu.OnDown = func(id interface{}) {
		h.OnButtonDown(id)
	}
	emu.OnUp = func(id interface{}) {
		h.OnButtonUp(id)
	}
	emu.OnRotate = func(id string, delta int) {
		h.OnRotate(id, delta)
	}
	emu.OnTouchStart = func(data device.TouchEventData) {
		h.OnTouchStart(data)
	}
	emu.OnTouchMove = func(data device.TouchEventData) {
		h.OnTouchMove(data)
	}
	emu.OnTouchEnd = func(data device.TouchEventData) {
		h.OnTouchEnd(data)
	}
}

// attachHardwareListeners binds connection, input, screen-update, state-change,
// and disconnect callbacks on hw to the handler.
func (h *BaseLoupeDeckHandler) attachHardwareListeners(hw *device.HardwareLoupedeckDevice) {
	hw.OnConnect = func(address string) {
		h.OnConnected(address)
	}
	hw.OnDown = func(id interface{}) {
		h.OnButtonDown(id)
		h.UpdateScreens(false)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	}
	hw.OnUp = func(id interface{}) {
		h.OnButtonUp(id)
		h.UpdateScreens(false)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	}
	hw.OnRotate = func(id string, delta int) {
		h.OnRotate(id, delta)
		h.UpdateScreens(false)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	}
	hw.OnTouchStart = func(data device.TouchEventData) {
		h.OnTouchStart(data)
		h.UpdateScreens(false)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	}
	hw.OnTouchMove = func(data device.TouchEventData) {
		h.OnTouchMove(data)
		h.UpdateScreens(false)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	}
	hw.OnTouchEnd = func(data device.TouchEventData) {
		h.OnTouchEnd(data)
		h.UpdateScreens(false)
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
	}
	hw.OnDisconnect = func(err error) {
		h.onHardwareDisconnect(err)
	}
}

// onHardwareDisconnect handles unexpected hardware disconnection. In hybrid
// mode it detaches only the hardware device so the emulator stays active;
// otherwise it stops the screen ticker and clears h.Device. When AutoReconnect
// is enabled and the handler is not stopping, it spawns connectToDeviceLoop.
func (h *BaseLoupeDeckHandler) onHardwareDisconnect(err error) {
	h.Mu.Lock()
	if h.Stopping {
		h.Mu.Unlock()
		return
	}
	if h.Hybrid && h.HybridDevice != nil {
		h.HardwareDevice = nil
		h.HardwareConnected = false
		h.HardwareAddress = ""
		h.HybridDevice.SetHardwareDevice(nil)
		autoRec := h.AutoReconnect
		h.Mu.Unlock()

		if err != nil {
			fmt.Printf("Connection to Loupedeck lost (%v)\n", err)
		}
		if h.OnStateChange != nil {
			h.OnStateChange()
		}
		if autoRec {
			go h.connectToDeviceLoop()
		}
		return
	}
	if h.tickerStopCh != nil {
		close(h.tickerStopCh)
		h.tickerStopCh = nil
	}
	h.Device = nil
	autoRec := h.AutoReconnect
	h.Mu.Unlock()

	if err != nil {
		fmt.Printf("Connection to Loupedeck lost (%v)\n", err)
	}
	if autoRec {
		go h.connectToDeviceLoop()
	}
}

// connectToDeviceLoop periodically discovers Loupedeck serial devices every
// ReconnectDelayMs and attempts to connect a matching candidate port (attaching
// it to HybridDevice in hybrid mode or as h.Device otherwise) until connection
// succeeds or stopCh/Stopping signals shutdown.
func (h *BaseLoupeDeckHandler) connectToDeviceLoop() {
	h.Mu.Lock()
	h.reconnecting = true
	h.Mu.Unlock()

	for {
		h.Mu.RLock()
		stopping := h.Stopping
		delay := h.ReconnectDelayMs
		isHybrid := h.Hybrid && h.HybridDevice != nil
		hybDev := h.HybridDevice
		h.Mu.RUnlock()
		if stopping {
			return
		}

		ports, err := device.DiscoverSerialDevices()
		if err == nil && len(ports) > 0 {
			selected := h.selectCandidatePort(ports)
			hw := device.NewHardwareLoupedeckDevice(selected)
			h.attachHardwareListeners(hw)
			if isHybrid {
				h.Mu.Lock()
				h.HardwareDevice = hw
				hybDev.SetHardwareDevice(hw)
				h.Mu.Unlock()
				if err := hw.Connect(); err == nil {
					h.Mu.Lock()
					h.reconnecting = false
					h.HardwareConnected = true
					h.HardwareAddress = selected.Path
					h.Mu.Unlock()
					if h.OnStateChange != nil {
						h.OnStateChange()
					}
					return
				}
				_ = hw.Close()
				h.Mu.Lock()
				h.HardwareDevice = nil
				h.HardwareConnected = false
				h.HardwareAddress = ""
				hybDev.SetHardwareDevice(nil)
				h.Mu.Unlock()
			} else {
				h.Mu.Lock()
				h.Device = hw
				h.Mu.Unlock()
				if err := hw.Connect(); err == nil {
					h.Mu.Lock()
					h.reconnecting = false
					h.Mu.Unlock()
					return
				}
				_ = hw.Close()
				h.Mu.Lock()
				h.Device = nil
				h.Mu.Unlock()
			}
		}

		select {
		case <-h.stopCh:
			return
		case <-time.After(time.Duration(delay) * time.Millisecond):
		}
	}
}

// selectCandidatePort chooses the best matching port from list based on the
// configured layout Path, SerialNumber, or DeviceID USB product IDs, falling
// back to list[0] if no specific preference matches.
func (h *BaseLoupeDeckHandler) selectCandidatePort(list []device.DiscoveredPort) device.DiscoveredPort {
	h.Mu.RLock()
	appCfg := h.AppConfig
	h.Mu.RUnlock()
	if appCfg == nil {
		return list[0]
	}
	cfgLayout := appCfg.Layout
	if cfgLayout.Path != "" {
		for _, p := range list {
			if p.Path == cfgLayout.Path {
				return p
			}
		}
	}
	if cfgLayout.SerialNumber != "" {
		for _, p := range list {
			if p.SerialNumber != "" && p.SerialNumber == cfgLayout.SerialNumber {
				return p
			}
		}
	}
	expectedSpec := config.ResolveDeviceSpec(cfgLayout.DeviceID)
	if expectedSpec != nil {
		for _, p := range list {
			for _, pid := range expectedSpec.ProductIDs {
				if p.ProductID == pid {
					return p
				}
			}
		}
	}
	return list[0]
}

// Stop gracefully shuts down the handler once, signaling stopCh and
// tickerStopCh, turning off brightness and closing the active device, stopping
// external interfaces, and exiting the process if ExitOnStop is set.
func (h *BaseLoupeDeckHandler) Stop() error {
	h.Mu.Lock()
	if h.Stopping {
		h.Mu.Unlock()
		return nil
	}
	h.Stopping = true
	select {
	case <-h.stopCh:
	default:
		close(h.stopCh)
	}
	if h.tickerStopCh != nil {
		close(h.tickerStopCh)
		h.tickerStopCh = nil
	}
	dev := h.Device
	h.Device = nil
	exitOnStop := h.ExitOnStop
	h.Mu.Unlock()

	if dev != nil {
		_ = dev.SetBrightness(0)
		_ = dev.Close()
		fmt.Println("Device Closed")
	}

	fmt.Println("Stopping interfaces")
	h.Ifaces.StopInterfaces()

	if exitOnStop {
		os.Exit(0)
	}
	return nil
}

// Vibrate sends the specified haptic feedback pattern to the active device, or
// returns nil if no device is currently attached.
func (h *BaseLoupeDeckHandler) Vibrate(pattern byte) error {
	fmt.Printf("Vibrate with pattern %d\n", pattern)
	h.Mu.RLock()
	dev := h.Device
	h.Mu.RUnlock()
	if dev == nil {
		return nil
	}
	return dev.Vibrate(pattern)
}

// ChangeBrightness clamps brightness to [0, 1], stores it on the handler, and
// forwards the value to the active device if one is attached.
func (h *BaseLoupeDeckHandler) ChangeBrightness(brightness float64) error {
	norm := math.Max(0, math.Min(1, brightness))
	fmt.Printf("Change Brightness to %v\n", norm)
	h.Mu.Lock()
	h.Brightness = norm
	dev := h.Device
	h.Mu.Unlock()
	if dev == nil {
		return nil
	}
	return dev.SetBrightness(norm)
}

// syncProfileButtonsState synchronizes physical profile-selection buttons in
// h.Buttons with h.CurrentProfile.
func (h *BaseLoupeDeckHandler) syncProfileButtonsState() {
	h.Mu.RLock()
	btns := h.Buttons
	curProf := h.CurrentProfile
	h.Mu.RUnlock()
	if btns != nil {
		btns.SyncProfileButtons(curProf)
	}
}

// GetCurrentProfile returns the active *config.Profile in a thread-safe manner,
// or nil if no configuration is loaded or CurrentProfile is out of range.
func (h *BaseLoupeDeckHandler) GetCurrentProfile() *config.Profile {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	if h.AppConfig == nil || h.CurrentProfile < 0 || h.CurrentProfile >= len(h.AppConfig.Profiles) {
		return nil
	}
	return h.AppConfig.Profiles[h.CurrentProfile]
}

// evaluateDeviceAndLayout compares the configured profile/application layout
// against dev, computes effective grid dimensions and mismatch warnings, stores
// the resulting LayoutCompatibility on the handler, and returns it.
func (h *BaseLoupeDeckHandler) evaluateDeviceAndLayout(dev device.Device, prof *config.Profile) LayoutCompatibility {
	h.Mu.RLock()
	appCfg := h.AppConfig
	h.Mu.RUnlock()

	var activeLayout config.LayoutConfig
	if prof != nil && prof.Layout.DeviceID != "" {
		activeLayout = prof.Layout
	} else if appCfg != nil {
		activeLayout = appCfg.Layout
	} else {
		activeLayout = config.NormalizeLayoutConfig(nil)
	}

	expectedSpec := config.ResolveDeviceSpec(activeLayout.DeviceID)
	if expectedSpec == nil {
		expectedSpec = config.ResolveDeviceSpec(activeLayout.Device)
	}
	var connectedSpec *config.DeviceSpec
	if dev != nil {
		connectedSpec = config.ResolveDeviceSpec(dev.GetModel())
		if connectedSpec == nil {
			connectedSpec = config.ResolveDeviceSpec(dev.GetType())
		}
	}

	primaryCols := activeLayout.Columns
	if primaryCols <= 0 && expectedSpec != nil {
		primaryCols = expectedSpec.Columns
	}
	if primaryCols <= 0 {
		primaryCols = 4
	}
	primaryRows := activeLayout.Rows
	if primaryRows <= 0 && expectedSpec != nil {
		primaryRows = expectedSpec.Rows
	}
	if primaryRows <= 0 {
		primaryRows = 3
	}

	devCols := primaryCols
	devRows := primaryRows
	if dev != nil {
		if dev.GetColumns() > 0 {
			devCols = dev.GetColumns()
		}
		if dev.GetRows() > 0 {
			devRows = dev.GetRows()
		}
	}

	warnings := []string{}
	matchesPrimary := true

	if expectedSpec != nil && connectedSpec != nil && expectedSpec.ID != connectedSpec.ID {
		matchesPrimary = false
		devType := connectedSpec.Type
		if dev != nil && dev.GetType() != "" {
			devType = dev.GetType()
		}
		warnings = append(warnings, fmt.Sprintf(
			`Configured primary device is "%s" (%dx%d), but connected device is "%s" (%dx%d). Preserving primary layout (%d columns x %d rows) as far as possible.`,
			expectedSpec.Type, primaryCols, primaryRows, devType, devCols, devRows, primaryCols, primaryRows,
		))
	} else if devCols != primaryCols || devRows != primaryRows {
		matchesPrimary = false
		devType := "Unknown"
		if dev != nil && dev.GetType() != "" {
			devType = dev.GetType()
		}
		warnings = append(warnings, fmt.Sprintf(
			`Connected device "%s" has a %dx%d touch grid, which differs from configured primary layout (%d columns x %d rows). Preserving %dx%d layout.`,
			devType, devCols, devRows, primaryCols, primaryRows, primaryCols, primaryRows,
		))
	}

	if prof != nil && dev != nil {
		displays := dev.GetDisplays()
		if len(prof.Touch.Left) > 0 {
			if _, ok := displays["left"]; !ok {
				warnings = append(warnings, fmt.Sprintf(`Profile "%s" configures touch.left, which is not available on connected device "%s".`, prof.Name, dev.GetType()))
			}
		}
		if len(prof.Touch.Right) > 0 {
			if _, ok := displays["right"]; !ok {
				warnings = append(warnings, fmt.Sprintf(`Profile "%s" configures touch.right, which is not available on connected device "%s".`, prof.Name, dev.GetType()))
			}
		}
	}

	primaryDev := activeLayout.Device
	if expectedSpec != nil {
		primaryDev = expectedSpec.Type
	}
	if primaryDev == "" {
		primaryDev = "Loupedeck Live"
	}
	primaryDevID := activeLayout.DeviceID
	if expectedSpec != nil {
		primaryDevID = expectedSpec.ID
	}
	if primaryDevID == "" {
		primaryDevID = "loupedeck-live"
	}
	connDev := "Disconnected"
	if dev != nil && dev.GetType() != "" {
		connDev = dev.GetType()
	}
	var connDevID *string
	if connectedSpec != nil {
		idCopy := connectedSpec.ID
		connDevID = &idCopy
	}

	compat := LayoutCompatibility{
		MatchesPrimaryDevice: matchesPrimary,
		PrimaryDevice:        primaryDev,
		PrimaryDeviceID:      primaryDevID,
		ConnectedDevice:      connDev,
		ConnectedDeviceID:    connDevID,
		PrimaryColumns:       primaryCols,
		PrimaryRows:          primaryRows,
		DeviceColumns:        devCols,
		DeviceRows:           devRows,
		EffectiveColumns:     primaryCols,
		EffectiveRows:        primaryRows,
		Warnings:             warnings,
	}

	h.Mu.Lock()
	h.LayoutCompatibility = compat
	h.Mu.Unlock()
	return compat
}

// ActivateProfile switches to profile index id (serialized by activateMu),
// evaluates layout compatibility, builds and preloads ButtonFields for all
// displays, knobs, and physical buttons, atomically swaps them in under
// updateMu/Mu, renders all fields to the device, schedules a post-switch screen
// refresh, and initializes profile-scoped external interfaces.
func (h *BaseLoupeDeckHandler) ActivateProfile(id int) error {
	h.activateMu.Lock()
	defer h.activateMu.Unlock()

	h.Mu.Lock()
	if h.AppConfig == nil || id < 0 || id >= len(h.AppConfig.Profiles) {
		h.Mu.Unlock()
		return nil
	}
	h.CurrentProfile = id
	dev := h.Device
	prof := h.AppConfig.Profiles[id]
	h.Mu.Unlock()

	if dev == nil || prof == nil {
		return nil
	}
	displays := dev.GetDisplays()
	if len(displays) == 0 {
		return nil
	}

	fmt.Printf("ACTIVATE PROFILE %d %s\n", id, prof.Name)
	compat := h.evaluateDeviceAndLayout(dev, prof)
	for _, w := range compat.Warnings {
		fmt.Printf("⚠️  [Layout/Device Warning] %s\n", w)
	}

	effectiveRows := compat.EffectiveRows
	if effectiveRows <= 0 {
		effectiveRows = 3
	}
	effectiveCols := compat.EffectiveColumns
	if effectiveCols <= 0 {
		effectiveCols = 4
	}

	var nextCenter, nextLeft, nextRight, nextKnobs, nextButtons *ButtonField

	if dCenter, ok := displays["center"]; ok {
		devCols := dev.GetColumns()
		if devCols <= 0 {
			devCols = effectiveCols
		}
		devRows := dev.GetRows()
		if devRows <= 0 {
			devRows = effectiveRows
		}
		centerOffset := dCenter.Offset[0]
		visX := dev.GetVisibleX()
		offsetX := math.Max(0, float64(visX[0]-centerOffset))
		visWidth := float64(dCenter.Width)
		if visX[1] > visX[0] && centerOffset == 0 {
			visWidth = float64(visX[1] - visX[0])
		}

		var hwCellWidth, hwCellHeight float64
		if dev.GetKeySize() > 0 {
			hwCellWidth = float64(dev.GetKeySize())
			hwCellHeight = float64(dev.GetKeySize())
		} else {
			hwCellWidth = visWidth / float64(devCols)
			hwCellHeight = float64(dCenter.Height) / float64(devRows)
		}

		cellWidth := hwCellWidth
		if devCols < effectiveCols && effectiveCols > 0 {
			cellWidth = visWidth / float64(effectiveCols)
		}
		cellHeight := hwCellHeight
		if devRows < effectiveRows && effectiveRows > 0 {
			cellHeight = float64(dCenter.Height) / float64(effectiveRows)
		}

		centerData := prof.Touch.Center
		nextCenter = NewButtonField(
			"center",
			effectiveRows,
			effectiveCols,
			dCenter.Width,
			dCenter.Height,
			centerData,
			prof,
			ButtonFieldOptions{CellWidth: cellWidth, CellHeight: cellHeight, OffsetX: offsetX, OffsetY: 0},
			h.Ifaces,
		)
		nextCenter.Load()
	}

	if dLeft, ok := displays["left"]; ok {
		leftData := prof.Touch.Left
		nextLeft = NewButtonField("left", 1, 1, dLeft.Width, dLeft.Height, leftData, prof, ButtonFieldOptions{}, h.Ifaces)
		nextLeft.Load()
	}

	if dRight, ok := displays["right"]; ok {
		rightData := prof.Touch.Right
		nextRight = NewButtonField("right", 1, 1, dRight.Width, dRight.Height, rightData, prof, ButtonFieldOptions{}, h.Ifaces)
		nextRight.Load()
	}

	nextKnobs = NewButtonField("knob", 1, 1, 0, 0, prof.Knobs, prof, ButtonFieldOptions{}, h.Ifaces)
	nextKnobs.Load()

	nextButtons = NewButtonField("buttons", 1, 1, 0, 0, prof.Buttons, prof, ButtonFieldOptions{}, h.Ifaces)
	nextButtons.Load()

	h.updateMu.Lock()
	h.Mu.Lock()
	if h.Stopping || h.Device != dev {
		h.Mu.Unlock()
		h.updateMu.Unlock()
		return nil
	}
	h.updatingScreens = true
	h.Screens["center"] = nextCenter
	h.Screens["left"] = nextLeft
	h.Screens["right"] = nextRight
	h.Knobs = nextKnobs
	h.Buttons = nextButtons
	h.Mu.Unlock()

	h.syncProfileButtonsState()

	if nextButtons != nil {
		_ = nextButtons.Draw(dev)
	}
	if nextLeft != nil {
		_ = nextLeft.Draw(dev)
	}
	if nextRight != nil {
		_ = nextRight.Draw(dev)
	}
	if nextCenter != nil {
		_ = nextCenter.Draw(dev)
	}

	h.Mu.Lock()
	h.postSwitchRefreshRemaining = 1
	h.postSwitchTickCounter = 0
	h.updatingScreens = false
	h.Mu.Unlock()
	h.updateMu.Unlock()

	h.Ifaces.InitializeProfile(prof)
	return nil
}

// OnConnected runs post-connection initialization when a device connects at
// address: updates hybrid hardware status, restores brightness, activates the
// active profile, triggers a startup haptic pulse, starts the 50 ms screen
// refresh ticker goroutine, and invokes OnStateChange.
func (h *BaseLoupeDeckHandler) OnConnected(address string) {
	h.Mu.RLock()
	dev := h.Device
	stopping := h.Stopping
	emulated := h.Emulated
	hybrid := h.Hybrid
	brightness := h.Brightness
	curProf := h.CurrentProfile
	numProfs := 1
	if h.AppConfig != nil && len(h.AppConfig.Profiles) > 0 {
		numProfs = len(h.AppConfig.Profiles)
	}
	h.Mu.RUnlock()

	if dev == nil || stopping {
		return
	}

	if address != "emulator://localhost" && hybrid {
		h.Mu.Lock()
		h.HardwareConnected = true
		h.HardwareAddress = address
		h.reconnecting = false
		h.Mu.Unlock()
	}

	fmt.Printf("✅ Connected to %s, %s\n", dev.GetType(), address)

	isHardwareConnect := !emulated || address != "emulator://localhost"
	if isHardwareConnect {
		time.Sleep(20 * time.Millisecond)
	}

	targetBrightness := brightness
	if targetBrightness <= 0 {
		targetBrightness = 1.0
	}
	_ = h.ChangeBrightness(targetBrightness)

	targetProf := curProf
	if targetProf < 0 || targetProf >= numProfs {
		targetProf = 0
	}
	_ = h.ActivateProfile(targetProf)

	_ = h.ChangeBrightness(targetBrightness)
	_ = h.Vibrate(device.HAPTIC["ASCEND_MED"])

	if isHardwareConnect {
		go func(tb float64) {
			time.Sleep(300 * time.Millisecond)
			h.Mu.RLock()
			stoppingNow := h.Stopping
			h.Mu.RUnlock()
			if !stoppingNow {
				_ = h.ChangeBrightness(tb)
			}
		}(targetBrightness)
	}

	fmt.Println("✅ Force Screen update")

	h.Mu.Lock()
	if h.tickerStopCh != nil {
		close(h.tickerStopCh)
	}
	tickerStop := make(chan struct{})
	h.tickerStopCh = tickerStop
	h.Mu.Unlock()

	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-tickerStop:
				return
			case <-h.stopCh:
				return
			case <-ticker.C:
				h.UpdateScreens(false)
			}
		}
	}()

	fmt.Println("✅ Done initializing")

	if h.OnStateChange != nil {
		h.OnStateChange()
	}
}

// UpdateScreens acquires updateMu non-blockingly and redraws physical button
// LEDs and touch screens when force is true, during post-switch refresh ticks,
// when a ButtonField is dirty, or on blink intervals (every 5 ticks for center,
// every 20 ticks for left/right). It invokes OnStateChange if any field was drawn.
func (h *BaseLoupeDeckHandler) UpdateScreens(force bool) {
	if !h.updateMu.TryLock() {
		return
	}
	defer h.updateMu.Unlock()

	h.Mu.Lock()
	dev := h.Device
	if dev == nil || h.updatingScreens {
		h.Mu.Unlock()
		return
	}
	h.updatingScreens = true
	h.Counter++
	counter := h.Counter

	postSwitchForce := false
	if h.postSwitchRefreshRemaining > 0 {
		h.postSwitchTickCounter++
		if h.postSwitchTickCounter >= 5 {
			postSwitchForce = true
			h.postSwitchRefreshRemaining--
			h.postSwitchTickCounter = 0
		}
	}

	btns := h.Buttons
	left := h.Screens["left"]
	right := h.Screens["right"]
	center := h.Screens["center"]
	h.Mu.Unlock()

	defer func() {
		h.Mu.Lock()
		h.updatingScreens = false
		h.Mu.Unlock()
	}()

	displays := dev.GetDisplays()
	anyDrawn := false

	if btns != nil {
		if force || postSwitchForce || btns.IsDirty() {
			h.syncProfileButtonsState()
			_ = btns.Draw(dev)
			anyDrawn = true
		}
	}
	if _, hasLeft := displays["left"]; hasLeft && left != nil {
		if force || postSwitchForce || left.IsDirty() || (counter%20 == 0 && left.HasBlinkingButtons()) {
			_ = left.Draw(dev)
			anyDrawn = true
		}
	}
	if _, hasRight := displays["right"]; hasRight && right != nil {
		if force || postSwitchForce || right.IsDirty() || (counter%20 == 0 && right.HasBlinkingButtons()) {
			_ = right.Draw(dev)
			anyDrawn = true
		}
	}
	if _, hasCenter := displays["center"]; hasCenter && center != nil {
		if force || postSwitchForce || center.IsDirty() || (counter%5 == 0 && center.HasBlinkingButtons()) {
			_ = center.Draw(dev)
			anyDrawn = true
		}
	}

	if anyDrawn && h.OnStateChange != nil {
		h.OnStateChange()
	}
}

// isIntegerButtonID reports whether id represents an integer physical button
// identifier (int, int64, integral float64, or numeric string) and returns its
// normalized decimal string representation.
func isIntegerButtonID(id interface{}) (string, bool) {
	switch v := id.(type) {
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		if v == math.Trunc(v) {
			return strconv.Itoa(int(v)), true
		}
	case string:
		if _, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), true
		}
	}
	return config.ToString(id), false
}

// OnButtonDown routes a press event for id to Buttons (if id is an integer
// physical button ID, syncing profile button LEDs and redrawing if the active
// profile did not change) or Knobs (for rotary encoder press IDs).
func (h *BaseLoupeDeckHandler) OnButtonDown(id interface{}) bool {
	idStr, isInt := isIntegerButtonID(id)
	h.Mu.RLock()
	btns := h.Buttons
	knobs := h.Knobs
	prevProfile := h.CurrentProfile
	dev := h.Device
	h.Mu.RUnlock()

	ok := false
	if isInt {
		if btns != nil {
			ok = btns.Pressed(idStr)
			h.syncProfileButtonsState()
			h.Mu.RLock()
			sameProf := h.CurrentProfile == prevProfile
			updating := h.updatingScreens
			h.Mu.RUnlock()
			if sameProf && dev != nil && !updating {
				if h.updateMu.TryLock() {
					_ = btns.Draw(dev)
					h.updateMu.Unlock()
				}
			}
		}
	} else {
		if knobs != nil {
			ok = knobs.Pressed(idStr)
		}
	}
	return ok
}

// OnButtonUp routes a release event for id to Buttons (for integer physical
// button IDs, syncing profile LEDs and redrawing) or Knobs (otherwise).
func (h *BaseLoupeDeckHandler) OnButtonUp(id interface{}) bool {
	idStr, isInt := isIntegerButtonID(id)
	h.Mu.RLock()
	btns := h.Buttons
	knobs := h.Knobs
	dev := h.Device
	h.Mu.RUnlock()

	ok := false
	if isInt {
		if btns != nil {
			ok = btns.Released(idStr)
			h.syncProfileButtonsState()
			h.Mu.RLock()
			updating := h.updatingScreens
			h.Mu.RUnlock()
			if dev != nil && !updating {
				if h.updateMu.TryLock() {
					_ = btns.Draw(dev)
					h.updateMu.Unlock()
				}
			}
		}
	} else {
		if knobs != nil {
			ok = knobs.Released(idStr)
		}
	}
	return ok
}

// OnRotate routes a rotary encoder step delta for control id to Knobs.
func (h *BaseLoupeDeckHandler) OnRotate(id string, delta int) bool {
	h.Mu.RLock()
	knobs := h.Knobs
	h.Mu.RUnlock()
	if knobs != nil {
		return knobs.Rotated(id, delta)
	}
	return false
}

// resolveTouchTarget resolves a hardware TouchPoint into its target screen
// name (defaulting to "center"), mapped configuration key, and validity flag.
// For the center screen it remaps hardware grid coordinates onto the effective
// ButtonField layout via MapHardwareKeyToConfigKey.
func (h *BaseLoupeDeckHandler) resolveTouchTarget(tp device.TouchPoint) (string, string, bool) {
	screen := tp.Target.Screen
	if screen == "" {
		screen = "center"
	}
	idStr := strconv.Itoa(tp.Target.Key)
	if screen == "center" {
		h.Mu.RLock()
		center := h.Screens["center"]
		dev := h.Device
		h.Mu.RUnlock()
		if center != nil && dev != nil {
			mapped, ok := center.MapHardwareKeyToConfigKey(tp.Target.Key, dev.GetColumns(), dev.GetRows())
			if !ok {
				return "", "", false
			}
			idStr = mapped
		}
	}
	return screen, idStr, true
}

// OnTouchStart dispatches touch-press events from evt.ChangedTouches to their
// resolved screen ButtonFields and returns true if any button handled the press.
func (h *BaseLoupeDeckHandler) OnTouchStart(evt device.TouchEventData) bool {
	ok := false
	for _, tp := range evt.ChangedTouches {
		screen, idStr, valid := h.resolveTouchTarget(tp)
		if !valid || screen == "" {
			continue
		}
		h.Mu.RLock()
		bf := h.Screens[screen]
		h.Mu.RUnlock()
		if bf != nil {
			ok = bf.Pressed(idStr) || ok
		}
	}
	return ok
}

// OnTouchMove dispatches touch-move coordinates from evt.ChangedTouches to
// their resolved screen ButtonFields.
func (h *BaseLoupeDeckHandler) OnTouchMove(evt device.TouchEventData) bool {
	ok := false
	for _, tp := range evt.ChangedTouches {
		screen, idStr, valid := h.resolveTouchTarget(tp)
		if !valid || screen == "" {
			continue
		}
		h.Mu.RLock()
		bf := h.Screens[screen]
		h.Mu.RUnlock()
		if bf != nil {
			ok = bf.TouchMove(idStr, tp.X, tp.Y) || ok
		}
	}
	return ok
}

// OnTouchEnd dispatches touch-release events from evt.ChangedTouches to their
// resolved screen ButtonFields and returns true if any button handled the release.
func (h *BaseLoupeDeckHandler) OnTouchEnd(evt device.TouchEventData) bool {
	ok := false
	for _, tp := range evt.ChangedTouches {
		screen, idStr, valid := h.resolveTouchTarget(tp)
		if !valid || screen == "" {
			continue
		}
		h.Mu.RLock()
		bf := h.Screens[screen]
		h.Mu.RUnlock()
		if bf != nil {
			ok = bf.Released(idStr) || ok
		}
	}
	return ok
}

// ButtonStateChanged propagates an external state or attribute update (for
// example from OPC UA monitoring) to the specified field, or across all fields
// when field is empty, immediately redrawing physical Buttons when updated.
func (h *BaseLoupeDeckHandler) ButtonStateChanged(buttonID, attribute, nodeid string, val interface{}, field string) bool {
	h.Mu.RLock()
	center := h.Screens["center"]
	left := h.Screens["left"]
	right := h.Screens["right"]
	knobs := h.Knobs
	btns := h.Buttons
	dev := h.Device
	h.Mu.RUnlock()

	ok := false
	if (field == "" || field == "center") && center != nil {
		ok = center.ButtonStateChanged(buttonID, attribute, nodeid, val) || ok
	}
	if (field == "" || field == "left") && left != nil {
		ok = left.ButtonStateChanged(buttonID, attribute, nodeid, val) || ok
	}
	if (field == "" || field == "right") && right != nil {
		ok = right.ButtonStateChanged(buttonID, attribute, nodeid, val) || ok
	}
	if (field == "" || field == "knobs") && knobs != nil {
		ok = knobs.ButtonStateChanged(buttonID, attribute, nodeid, val) || ok
	}
	if (field == "" || field == "buttons") && btns != nil {
		ok = btns.ButtonStateChanged(buttonID, attribute, nodeid, val) || ok
		if dev != nil && h.updateMu.TryLock() {
			_ = btns.Draw(dev)
			h.updateMu.Unlock()
		}
	}
	return ok
}

// ReloadConfig reloads configuration from fileName (or the current ConfigFile
// when empty), clamps CurrentProfile to the loaded profile range, reactivates
// the profile, and forces a full screen redraw.
func (h *BaseLoupeDeckHandler) ReloadConfig(fileName string) bool {
	if fileName == "" {
		h.Mu.RLock()
		fileName = h.ConfigFile
		h.Mu.RUnlock()
	} else {
		h.Mu.Lock()
		h.ConfigFile = fileName
		h.Mu.Unlock()
	}
	ok := h.LoadConfig(fileName)
	h.Mu.RLock()
	dev := h.Device
	curProf := h.CurrentProfile
	numProfs := 0
	if h.AppConfig != nil {
		numProfs = len(h.AppConfig.Profiles)
	}
	h.Mu.RUnlock()

	if ok && dev != nil && numProfs > 0 {
		targetProfile := curProf
		if targetProfile >= numProfs {
			targetProfile = numProfs - 1
		}
		if targetProfile < 0 {
			targetProfile = 0
		}
		_ = h.ActivateProfile(targetProfile)
		h.UpdateScreens(true)
	}
	return ok
}

// SetEmulatedModel switches the emulated device (or the emulated side of
// HybridDevice in hybrid mode) to model, attaches event listeners, connects it,
// reactivates the current profile, and forces a screen redraw.
func (h *BaseLoupeDeckHandler) SetEmulatedModel(model string) error {
	emu := device.NewEmulatedLoupedeckDevice(device.EmulatedDeviceOptions{Model: model})
	h.attachEmulatedListeners(emu)
	h.Mu.Lock()
	if h.Hybrid && h.HybridDevice != nil {
		h.EmulatedDevice = emu
		h.HybridDevice.SetEmulatedDevice(emu)
		curProf := h.CurrentProfile
		h.Mu.Unlock()

		_ = emu.Connect()
		_ = h.ActivateProfile(curProf)
		h.UpdateScreens(true)
		return nil
	}
	h.Device = emu
	h.EmulatedDevice = emu
	h.Emulated = true
	curProf := h.CurrentProfile
	h.Mu.Unlock()

	_ = emu.Connect()
	_ = h.ActivateProfile(curProf)
	h.UpdateScreens(true)
	return nil
}

// GetSnapshot returns a thread-safe, JSON-serializable snapshot of the handler's
// configuration, layout compatibility, profiles, button/screen states, device
// or hybrid connection status, and OPC UA subscriptions.
func (h *BaseLoupeDeckHandler) GetSnapshot() map[string]interface{} {
	h.Mu.RLock()
	cfgFile := h.ConfigFile
	appCfg := h.AppConfig
	curProfIdx := h.CurrentProfile
	brightness := h.Brightness
	compat := h.LayoutCompatibility
	center := h.Screens["center"]
	left := h.Screens["left"]
	right := h.Screens["right"]
	knobs := h.Knobs
	btns := h.Buttons
	dev := h.Device
	emu := h.EmulatedDevice
	reconnecting := h.reconnecting
	hybrid := h.Hybrid
	hwConn := h.HardwareConnected
	hwAddr := h.HardwareAddress
	h.Mu.RUnlock()

	var prof *config.Profile
	if appCfg != nil && curProfIdx >= 0 && curProfIdx < len(appCfg.Profiles) {
		prof = appCfg.Profiles[curProfIdx]
	}

	appName := ""
	primaryDevice := "Loupedeck Live"
	var layoutVal interface{}
	profilesList := []map[string]interface{}{}

	if appCfg != nil {
		appName = appCfg.Application
		if appCfg.Device != "" {
			primaryDevice = appCfg.Device
		}
		layoutVal = appCfg.Layout
		for idx, p := range appCfg.Profiles {
			pDev := p.Device
			if pDev == "" {
				pDev = primaryDevice
			}
			var pLayout interface{} = appCfg.Layout
			if p.Layout.DeviceID != "" {
				pLayout = p.Layout
			}
			profilesList = append(profilesList, map[string]interface{}{
				"index":       idx,
				"name":        p.Name,
				"profile":     p.Profile,
				"description": p.Description,
				"device":      pDev,
				"layout":      pLayout,
			})
		}
	}
	if prof != nil && prof.Layout.DeviceID != "" {
		layoutVal = prof.Layout
	}
	activeProfName := ""
	if prof != nil {
		activeProfName = prof.Name
	}

	fieldsMap := map[string]interface{}{
		"center":  map[string]interface{}{},
		"left":    map[string]interface{}{},
		"right":   map[string]interface{}{},
		"knobs":   map[string]interface{}{},
		"buttons": map[string]interface{}{},
	}
	if center != nil {
		fieldsMap["center"] = center.GetSnapshot()
	}
	if left != nil {
		fieldsMap["left"] = left.GetSnapshot()
	}
	if right != nil {
		fieldsMap["right"] = right.GetSnapshot()
	}
	if knobs != nil {
		fieldsMap["knobs"] = knobs.GetSnapshot()
	}
	if btns != nil {
		fieldsMap["buttons"] = btns.GetSnapshot()
	}

	var devSnap interface{}
	if emu != nil {
		emuSnap := emu.GetSnapshot()
		if hybrid {
			if dev != nil {
				emuSnap["type"] = dev.GetType()
			}
			emuSnap["hybrid"] = true
			emuSnap["hardwareConnected"] = hwConn
			emuSnap["hardwareAddress"] = hwAddr
			emuSnap["reconnecting"] = reconnecting
		}
		devSnap = emuSnap
	} else if dev != nil {
		devSnap = map[string]interface{}{
			"type":         dev.GetType(),
			"connected":    true,
			"reconnecting": reconnecting,
		}
	} else {
		devSnap = map[string]interface{}{
			"type":         "Disconnected",
			"connected":    false,
			"reconnecting": reconnecting,
		}
	}

	return map[string]interface{}{
		"configFile":          cfgFile,
		"application":         appName,
		"primaryDevice":       primaryDevice,
		"layout":              layoutVal,
		"layoutCompatibility": compat,
		"currentProfile":      curProfIdx,
		"brightness":          brightness,
		"profiles":            profilesList,
		"activeProfileName":   activeProfName,
		"fields":              fieldsMap,
		"device":              devSnap,
		"hybrid":              hybrid,
		"opcua":               h.Ifaces.OPCUA.GetSubscriptionsSnapshot(),
	}
}
