package commander_test

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/commander"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/device"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/emulator"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/interfaces"
)

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

