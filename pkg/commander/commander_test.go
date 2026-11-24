// Package commander_test contains integration and unit tests for the
// commander, config, interfaces, device, and emulator packages.
package commander_test

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/commander"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/device"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/emulator"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/interfaces"
)

// resolveTestRepoRoot locates the repository root containing config/config.yaml
// relative to the pkg/commander test directory.
func resolveTestRepoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(abs, "config/config.yaml")); err != nil {
		t.Fatalf("could not locate config/config.yaml in %s: %v", abs, err)
	}
	return abs
}

// TestUtilsAndColorHelpers verifies value wrap-around arithmetic, color
// inversion/darkening, and template formatting helpers in pkg/config.
func TestUtilsAndColorHelpers(t *testing.T) {
	if got := config.CalcDelta(95, 10, 0, 100); got != 0 {
		t.Fatalf("expected CalcDelta overflow to wrap to 0, got %v", got)
	}
	if got := config.CalcDelta(5, -10, 0, 100); got != 100 {
		t.Fatalf("expected CalcDelta underflow to wrap to 100, got %v", got)
	}
	if got := config.InvertColor("#00ff00"); got != "#ff00ff" {
		t.Fatalf("expected InvertColor(#00ff00) == #ff00ff, got %s", got)
	}
	if got := config.DarkerColor("#00ff00", 0.5); got != "#008000" {
		t.Fatalf("expected DarkerColor(#00ff00, 0.5) == #008000, got %s", got)
	}
	tpl := config.FormatTemplate("ns=1;s=saph.kvm.in{key}.Connection", map[string]any{"key": 3})
	if tpl != "ns=1;s=saph.kvm.in3.Connection" {
		t.Fatalf("unexpected FormatTemplate result: %s", tpl)
	}
}

// TestConfigValidatorAndAutoFix verifies validation of config/config.yaml and
// automatic repair of legacy profile structures via config.AutoFixProfileData.
func TestConfigValidatorAndAutoFix(t *testing.T) {
	repoRoot := resolveTestRepoRoot(t)
	report := config.ValidateAppConfigFile("config/config.yaml", repoRoot)
	if !report.Valid {
		t.Fatalf("expected config/config.yaml to be valid, got errors: %+v", report.Errors)
	}
	if report.Summary.ProfileCount != 2 {
		t.Fatalf("expected 2 profiles, got %d", report.Summary.ProfileCount)
	}

	legacyProfile := map[string]any{
		"profile":    "Legacy Test",
		"parameters": map[string]any{"hostname": "127.0.0.1"},
		"touch": map[string]any{
			"center": map[string]any{
				"0": map[string]any{
					"text":    "LegacyText",
					"default": "off",
					"states": map[string]any{
						"off": map[string]any{
							"color":     "#112233",
							"textcolor": "#ffffff",
						},
					},
				},
			},
		},
	}
	fixRes := config.AutoFixProfileData(legacyProfile)
	if len(fixRes.FixesApplied) < 3 {
		t.Fatalf("expected at least 3 auto-fixes, got %v", fixRes.FixesApplied)
	}
}

// TestShellAndHttpSanitization verifies that shell metacharacters are stripped
// from template-substituted values during dry-run command execution.
func TestShellAndHttpSanitization(t *testing.T) {
	var lastActivity interfaces.ActivityEntry
	sh := interfaces.NewSHELLif()
	sh.SetDryRun(true)
	sh.SetActivityEmitter(func(e interfaces.ActivityEntry) {
		lastActivity = e
	})

	opts := map[string]any{
		"id":    "touch-0",
		"key":   "0",
		"state": "on",
		"min":   0,
		"max":   100,
		"color": "#000000",
		"image": "",
		"value": "hello; rm -rf /",
	}
	_, err := sh.Call("echo {value}", opts)
	if err != nil {
		t.Fatal(err)
	}
	if lastActivity.Formatted != "echo hello rm -rf /" {
		t.Fatalf("expected shell metacharacters to be stripped, got %q", lastActivity.Formatted)
	}
}

// TestHandlerProfileSwitchingAndOPCUASubscriptionUpdate tests profile LED
// initialization, simulated OPC UA node updates, screen rendering updates, and
// physical button profile switching against an emulated device.
func TestHandlerProfileSwitchingAndOPCUASubscriptionUpdate(t *testing.T) {
	repoRoot := resolveTestRepoRoot(t)
	cfgPath := filepath.Join(repoRoot, "config/config.yaml")

	emu := device.NewEmulatedLoupedeckDevice(device.EmulatedDeviceOptions{Model: "live"})
	handler, err := commander.NewBaseLoupeDeckHandler(cfgPath, commander.HandlerOptions{
		Device:     emu,
		Emulate:    true,
		DryRun:     true,
		ExitOnStop: false,
		BaseDir:    repoRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Stop()

	if err := handler.Start(); err != nil {
		t.Fatal(err)
	}

	snap := handler.GetSnapshot()
	if snap["currentProfile"] != 0 {
		t.Fatalf("expected initial profile 0, got %v", snap["currentProfile"])
	}

	// Physical button 1 corresponds to profile 0 (active -> green rgba(0, 255, 0)), button 2 to profile 1 (inactive -> blue rgba(0, 0, 85))
	devSnap := emu.GetSnapshot()
	btnColors := devSnap["buttonColors"].(map[string]string)
	if btnColors["1"] != "rgba(0, 255, 0)" {
		t.Fatalf("expected button 1 LED to be rgba(0, 255, 0), got %q", btnColors["1"])
	}
	if btnColors["2"] != "rgba(0, 0, 85)" {
		t.Fatalf("expected button 2 LED to be rgba(0, 0, 85), got %q", btnColors["2"])
	}

	// Simulate OPC UA node change on profile 0 (lights): ns=2;s=Home.Lights.LivingRoom.State -> "1"
	beforeCenterURL := emu.GetScreenDataURL("center")
	pushed := handler.Ifaces.OPCUA.SimulateNodeValue("ns=2;s=Home.Lights.LivingRoom.State", "1")
	if len(pushed) == 0 {
		t.Fatalf("expected SimulateNodeValue to notify at least 1 button")
	}
	handler.UpdateScreens(false)
	afterCenterURL := emu.GetScreenDataURL("center")
	if beforeCenterURL == afterCenterURL {
		t.Fatalf("expected center screen dataURL to update after OPC UA state change")
	}

	snapAfter := handler.GetSnapshot()
	fields := snapAfter["fields"].(map[string]any)
	centerField := fields["center"].(map[string]any)
	btn0 := centerField["0"].(map[string]any)
	if btn0["currentState"] != "1" {
		t.Fatalf("expected center button 0 state to be '1', got %v", btn0["currentState"])
	}

	// Switch to profile 1 (sockets) via physical button 2 press
	emu.TriggerButtonDown(2)
	emu.TriggerButtonUp(2)
	handler.UpdateScreens(false)

	if handler.CurrentProfile != 1 {
		t.Fatalf("expected CurrentProfile == 1 after pressing button 2, got %d", handler.CurrentProfile)
	}
	devSnap2 := emu.GetSnapshot()
	btnColors2 := devSnap2["buttonColors"].(map[string]string)
	if btnColors2["2"] != "rgba(0, 255, 0)" {
		t.Fatalf("expected button 2 LED to be rgba(0, 255, 0) on profile 1, got %q", btnColors2["2"])
	}
}

// TestEmulatorServerEndpoints starts the emulator HTTP server on an ephemeral
// port and verifies GET /api/state and POST /api/opcua/simulate behavior.
func TestEmulatorServerEndpoints(t *testing.T) {
	repoRoot := resolveTestRepoRoot(t)
	dryRun := true
	srv, err := emulator.StartEmulatorServer(emulator.ServerOptions{
		Config:  "config/config.yaml",
		Port:    0, // random free port
		Host:    "127.0.0.1",
		DryRun:  &dryRun,
		BaseDir: repoRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	baseURL := "http://127.0.0.1:" + config.ToString(srv.Port)

	// 1. GET /api/state
	resp, err := http.Get(baseURL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/state, got %d", resp.StatusCode)
	}
	var statePayload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&statePayload); err != nil {
		t.Fatal(err)
	}
	if statePayload["snapshot"] == nil {
		t.Fatalf("expected snapshot in /api/state response")
	}

	// 2. POST /api/opcua/simulate
	simBody, _ := json.Marshal(map[string]any{
		"nodeId": "ns=2;s=Home.Lights.LivingRoom.State",
		"value":  "1",
	})
	simResp, err := http.Post(baseURL+"/api/opcua/simulate", "application/json", bytes.NewReader(simBody))
	if err != nil {
		t.Fatal(err)
	}
	defer simResp.Body.Close()
	if simResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/opcua/simulate, got %d", simResp.StatusCode)
	}
	var simPayload map[string]any
	if err := json.NewDecoder(simResp.Body).Decode(&simPayload); err != nil {
		t.Fatal(err)
	}
	snap := simPayload["snapshot"].(map[string]any)
	fields := snap["fields"].(map[string]any)
	center := fields["center"].(map[string]any)
	btn0 := center["0"].(map[string]any)
	if btn0["currentState"] != "1" {
		t.Fatalf("expected center button 0 currentState == '1' after /api/opcua/simulate, got %v", btn0["currentState"])
	}
}

// TestLiveOPCUAConnectionAndSessionReuse exercises live OPC UA connection,
// write calls, and session reuse across re-Init when localhost:4840 is reachable.
func TestLiveOPCUAConnectionAndSessionReuse(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "localhost:4840", 200*time.Millisecond)
	if err != nil {
		t.Skip("local OPC-UA server on localhost:4840 not reachable; skipping live test")
	}
	_ = conn.Close()

	opc := interfaces.NewOPCUAIf()
	defer opc.Stop()

	opts := map[string]any{"endpointurl": "opc.tcp://localhost:4840"}
	start := time.Now()
	opc.Init(opts, nil)
	elapsed := time.Since(start)

	snap := opc.GetSubscriptionsSnapshot()
	if !snap.Connected {
		t.Fatalf("expected OPCUAIf to connect to opc.tcp://localhost:4840 during Init (elapsed %v), got connected=%v", elapsed, snap.Connected)
	}

	val, err := opc.Call("ns=2;s=IsBatt1.Kvm.in.Connection", map[string]any{
		"endpointurl": "opc.tcp://localhost:4840",
		"nodeid":      "ns=2;s=IsBatt1.Kvm.out.Connection",
		"id":          "center.0",
		"key":         "0",
		"state":       "0",
		"min":         0,
		"max":         100,
		"color":       "#000000",
		"image":       "",
		"value":       5,
	})
	if err != nil {
		t.Fatalf("expected OPCUAIf.Call write to succeed without 'client not connected', got err: %v", err)
	}
	if val != "waiting" {
		t.Fatalf("expected 'waiting' from OPCUAIf.Call write on connected server, got %v", val)
	}

	// Re-Init with same endpoint (simulating profile switch) and verify session remains connected immediately
	opc.Init(opts, nil)
	if !opc.GetSubscriptionsSnapshot().Connected {
		t.Fatalf("expected OPCUAIf session reuse on profile switch to stay connected")
	}
}

// mockPhysicalDevice is an in-memory thread-safe implementation of
// device.Device used to test hybrid-mode synchronization without real USB/serial
// hardware.
type mockPhysicalDevice struct {
	mu           sync.RWMutex
	connected    bool
	closed       bool
	brightness   float64
	vibrations   []byte
	buttonColors map[string]string
	screenDraws  map[string]int
	screenData   map[string]string
	displays     map[string]device.DisplayInfo
	buttons      []interface{}
	knobs        []string
}

// newMockPhysicalDevice constructs a mockPhysicalDevice configured with
// Loupedeck Live display dimensions, buttons, and knobs.
func newMockPhysicalDevice() *mockPhysicalDevice {
	return &mockPhysicalDevice{
		buttonColors: make(map[string]string),
		screenDraws:  make(map[string]int),
		screenData:   make(map[string]string),
		displays: map[string]device.DisplayInfo{
			"center": {Width: 360, Height: 270},
			"left":   {Width: 60, Height: 270},
			"right":  {Width: 60, Height: 270},
		},
		buttons: []interface{}{0, 1, 2, 3, 4, 5, 6, 7},
		knobs:   []string{"knobTL", "knobCL", "knobBL", "knobTR", "knobCR", "knobBR"},
	}
}

// GetType returns the mock device's human-readable model name.
func (m *mockPhysicalDevice) GetType() string { return "Loupedeck Live" }

// GetModel returns the mock device's short model identifier.
func (m *mockPhysicalDevice) GetModel() string { return "live" }

// GetRows returns the number of rows in the mock center touch grid.
func (m *mockPhysicalDevice) GetRows() int { return 3 }

// GetColumns returns the number of columns in the mock center touch grid.
func (m *mockPhysicalDevice) GetColumns() int { return 4 }

// GetKeySize returns the pixel dimension of each square touch tile.
func (m *mockPhysicalDevice) GetKeySize() int { return 90 }

// GetVisibleX returns the horizontal visible display range in pixels.
func (m *mockPhysicalDevice) GetVisibleX() [2]int { return [2]int{0, 480} }

// GetDisplays returns the mock device's display definitions.
func (m *mockPhysicalDevice) GetDisplays() map[string]device.DisplayInfo { return m.displays }

// GetButtons returns the supported physical button identifiers.
func (m *mockPhysicalDevice) GetButtons() []interface{} { return m.buttons }

// GetKnobs returns the supported rotary knob identifiers.
func (m *mockPhysicalDevice) GetKnobs() []string { return m.knobs }

// Connect marks the mock physical device as connected.
func (m *mockPhysicalDevice) Connect() error {
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	return nil
}

// Close marks the mock physical device as disconnected and closed.
func (m *mockPhysicalDevice) Close() error {
	m.mu.Lock()
	m.connected = false
	m.closed = true
	m.mu.Unlock()
	return nil
}

// SetBrightness records the latest brightness value on the mock device.
func (m *mockPhysicalDevice) SetBrightness(value float64) error {
	m.mu.Lock()
	m.brightness = value
	m.mu.Unlock()
	return nil
}

// Vibrate appends pattern to the recorded vibration history on the mock device.
func (m *mockPhysicalDevice) Vibrate(pattern byte) error {
	m.mu.Lock()
	m.vibrations = append(m.vibrations, pattern)
	m.mu.Unlock()
	return nil
}

// SetButtonColor records the LED color string for physical button id.
func (m *mockPhysicalDevice) SetButtonColor(id interface{}, color string) error {
	m.mu.Lock()
	m.buttonColors[config.ToString(id)] = color
	m.mu.Unlock()
	return nil
}

// DrawScreen invokes drawCallback on an offscreen canvas and stores the
// resulting PNG data URL and draw count for screenName.
func (m *mockPhysicalDevice) DrawScreen(screenName string, drawCallback func(ctx *device.Context2D) error) error {
	dim, ok := m.displays[screenName]
	if !ok {
		return nil
	}
	c := device.NewCanvas(dim.Width, dim.Height)
	ctx := c.GetContext2D()
	ctx.FillStyle = "#000000"
	ctx.FillRect(0, 0, float64(dim.Width), float64(dim.Height))
	if drawCallback != nil {
		if err := drawCallback(ctx); err != nil {
			return err
		}
	}
	dataURL := c.ToDataURL()
	m.mu.Lock()
	m.screenDraws[screenName]++
	m.screenData[screenName] = dataURL
	m.mu.Unlock()
	return nil
}

// GetScreenDataURL returns the last rendered data URL for screenName.
func (m *mockPhysicalDevice) GetScreenDataURL(screenName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.screenData[screenName]
}

// TestHybridModePhysicalAndEmulatedSync verifies that hybrid mode keeps
// emulated and physical devices synchronized across initial rendering,
// brightness, haptics, button-triggered profile switches, and hardware detach.
func TestHybridModePhysicalAndEmulatedSync(t *testing.T) {
	repoRoot := resolveTestRepoRoot(t)
	mockHW := newMockPhysicalDevice()
	dryRun := true

	srv, err := emulator.StartEmulatorServer(emulator.ServerOptions{
		Config:         "config/config.yaml",
		Port:           0,
		Host:           "127.0.0.1",
		DryRun:         &dryRun,
		BaseDir:        repoRoot,
		Hybrid:         true,
		HardwareDevice: mockHW,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	baseURL := "http://127.0.0.1:" + config.ToString(srv.Port)

	// 1. Verify /api/state reports hybrid: true and hardwareConnected: true
	resp, err := http.Get(baseURL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/state, got %d", resp.StatusCode)
	}
	var statePayload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&statePayload); err != nil {
		t.Fatal(err)
	}
	if statePayload["hybrid"] != true {
		t.Fatalf("expected /api/state hybrid == true, got %v", statePayload["hybrid"])
	}
	snap, ok := statePayload["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("expected snapshot object in /api/state")
	}
	if snap["hybrid"] != true {
		t.Fatalf("expected snapshot.hybrid == true, got %v", snap["hybrid"])
	}
	devSnap, ok := snap["device"].(map[string]any)
	if !ok {
		t.Fatalf("expected snapshot.device object in /api/state")
	}
	if devSnap["hybrid"] != true {
		t.Fatalf("expected snapshot.device.hybrid == true, got %v", devSnap["hybrid"])
	}
	if devSnap["hardwareConnected"] != true {
		t.Fatalf("expected snapshot.device.hardwareConnected == true, got %v", devSnap["hardwareConnected"])
	}

	// 2. Verify initial screen draws & button LED colors synced to both emulated and physical devices
	mockHW.mu.RLock()
	hwConnected := mockHW.connected
	hwBtn1 := mockHW.buttonColors["1"]
	hwBtn2 := mockHW.buttonColors["2"]
	hwCenterDraws := mockHW.screenDraws["center"]
	hwCenterURL := mockHW.screenData["center"]
	mockHW.mu.RUnlock()

	if !hwConnected {
		t.Fatalf("expected mock hardware device to be connected")
	}
	if hwCenterDraws == 0 || hwCenterURL == "" {
		t.Fatalf("expected center screen to be drawn on mock hardware device")
	}
	if hwCenterURL != srv.EmulatedDevice.GetScreenDataURL("center") {
		t.Fatalf("expected center screen data URL to match between emulated and hardware devices")
	}

	emuSnap := srv.EmulatedDevice.GetSnapshot()
	emuBtnColors := emuSnap["buttonColors"].(map[string]string)
	if hwBtn1 != "rgba(0, 255, 0)" || emuBtnColors["1"] != "rgba(0, 255, 0)" {
		t.Fatalf("expected button 1 LED rgba(0, 255, 0) on both devices, got hw=%q emu=%q", hwBtn1, emuBtnColors["1"])
	}
	if hwBtn2 != "rgba(0, 0, 85)" || emuBtnColors["2"] != "rgba(0, 0, 85)" {
		t.Fatalf("expected button 2 LED rgba(0, 0, 85) on both devices, got hw=%q emu=%q", hwBtn2, emuBtnColors["2"])
	}

	// 3. Verify brightness & vibration sync to both devices
	if err := srv.Handler.ChangeBrightness(0.55); err != nil {
		t.Fatal(err)
	}
	if err := srv.Handler.Vibrate(device.HAPTIC["SHORT"]); err != nil {
		t.Fatal(err)
	}
	mockHW.mu.RLock()
	hwBrightness := mockHW.brightness
	hwVibCount := len(mockHW.vibrations)
	hwLastVib := byte(0)
	if hwVibCount > 0 {
		hwLastVib = mockHW.vibrations[hwVibCount-1]
	}
	mockHW.mu.RUnlock()

	srv.EmulatedDevice.Mu.RLock()
	emuBrightness := srv.EmulatedDevice.Brightness
	emuLastVib := srv.EmulatedDevice.LastVibration
	srv.EmulatedDevice.Mu.RUnlock()

	if hwBrightness != 0.55 || emuBrightness != 0.55 {
		t.Fatalf("expected brightness 0.55 on both devices, got hw=%v emu=%v", hwBrightness, emuBrightness)
	}
	if hwVibCount == 0 || hwLastVib != device.HAPTIC["SHORT"] || emuLastVib == nil || emuLastVib.Pattern != device.HAPTIC["SHORT"] {
		t.Fatalf("expected vibration SHORT synced to both devices, got hw=%v emu=%+v", hwLastVib, emuLastVib)
	}

	// 4. Verify button input triggers profile switch and syncs LEDs and screens on both devices
	beforeCenterURL := srv.EmulatedDevice.GetScreenDataURL("center")
	srv.EmulatedDevice.TriggerButtonDown(2)
	srv.EmulatedDevice.TriggerButtonUp(2)
	srv.Handler.UpdateScreens(false)

	if srv.Handler.CurrentProfile != 1 {
		t.Fatalf("expected CurrentProfile == 1 after button 2 press, got %d", srv.Handler.CurrentProfile)
	}

	mockHW.mu.RLock()
	hwBtn2After := mockHW.buttonColors["2"]
	hwCenterAfter := mockHW.screenData["center"]
	mockHW.mu.RUnlock()
	emuBtnColorsAfter := srv.EmulatedDevice.GetSnapshot()["buttonColors"].(map[string]string)

	if hwBtn2After != "rgba(0, 255, 0)" || emuBtnColorsAfter["2"] != "rgba(0, 255, 0)" {
		t.Fatalf("expected button 2 LED rgba(0, 255, 0) on both devices after profile switch, got hw=%q emu=%q", hwBtn2After, emuBtnColorsAfter["2"])
	}
	if hwCenterAfter == beforeCenterURL || hwCenterAfter != srv.EmulatedDevice.GetScreenDataURL("center") {
		t.Fatalf("expected center screen to update and match on both devices after profile switch")
	}

	// 5. Verify waiting-for-hardware GetType() when hardware disconnects in hybrid mode
	srv.Handler.HybridDevice.SetHardwareDevice(nil)
	if gotType := srv.Handler.HybridDevice.GetType(); gotType != "Loupedeck Live (Hybrid - Waiting for Hardware)" {
		t.Fatalf("unexpected GetType() without hardware: %q", gotType)
	}
}
