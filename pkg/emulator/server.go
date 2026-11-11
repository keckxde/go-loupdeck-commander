package emulator

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/commander"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/device"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/interfaces"
	"gopkg.in/yaml.v3"
)

//go:embed emulator-ui.html
var emulatorUIHTML []byte

var iconExtRegex = regexp.MustCompile(`(?i)\.(png|jpg|jpeg|svg|gif)$`)

const maxBodyBytes = 1024 * 1024 // 1 MB

type ServerOptions struct {
	Config  string
	Port    int
	Host    string
	DryRun  *bool
	Model   string
	BaseDir string
}

type ServerInstance struct {
	HTTPServer     *http.Server
	Listener       net.Listener
	Handler        *commander.BaseLoupeDeckHandler
	EmulatedDevice *device.EmulatedLoupedeckDevice
	Port           int
}

func (s *ServerInstance) Close() error {
	if s.Handler != nil {
		_ = s.Handler.Stop()
	}
	if s.HTTPServer != nil {
		return s.HTTPServer.Close()
	}
	return nil
}

func listAvailableIcons(baseDir string) []string {
	var results []string
	for _, relDir := range []string{"icons", "data/icons"} {
		iconsDir := filepath.Join(baseDir, relDir)
		entries, err := os.ReadDir(iconsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && iconExtRegex.MatchString(entry.Name()) {
				results = append(results, relDir+"/"+entry.Name())
			}
		}
	}
	if results == nil {
		results = []string{}
	}
	return results
}

func readJSONBody(r *http.Request) (map[string]interface{}, error) {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, maxBodyBytes+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(b) > maxBodyBytes {
		return nil, fmt.Errorf("request body exceeds 1 MB limit")
	}
	if len(b) == 0 {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sendJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func StartEmulatorServer(opts ServerOptions) (*ServerInstance, error) {
	configFile := opts.Config
	if configFile == "" {
		configFile = "config/config.yaml"
	}
	port := opts.Port
	if port < 0 {
		port = 3080
	}
	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	dryRun := true
	if opts.DryRun != nil {
		dryRun = *opts.DryRun
	}
	baseDir := opts.BaseDir
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}
	baseDir, _ = filepath.Abs(baseDir)

	cfgPath := configFile
	if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(baseDir, configFile)
	}
	parsedAppData, _ := config.ParseConfigFileSafe(cfgPath)
	configuredModel := ""
	if parsedAppData != nil {
		configuredModel = config.ToString(parsedAppData["device"])
		if configuredModel == "" {
			configuredModel = config.ToString(parsedAppData["primaryDevice"])
		}
		if configuredModel == "" {
			if lm, ok := parsedAppData["layout"].(map[string]interface{}); ok {
				configuredModel = config.ToString(lm["device"])
				if configuredModel == "" {
					configuredModel = config.ToString(lm["model"])
				}
			}
		}
	}
	model := opts.Model
	if model == "" {
		model = configuredModel
	}
	if model == "" {
		model = "live"
	}

	var sseMu sync.Mutex
	sseClients := make(map[chan string]struct{})

	broadcastSSE := func(eventName string, data interface{}) {
		b, err := json.Marshal(data)
		if err != nil {
			return
		}
		msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventName, string(b))
		sseMu.Lock()
		defer sseMu.Unlock()
		for ch := range sseClients {
			select {
			case ch <- msg:
			default:
			}
		}
	}

	emulatedDevice := device.NewEmulatedLoupedeckDevice(device.EmulatedDeviceOptions{Model: model})
	emulatedDevice.OnStateChange = func(change device.StateChangeEvent) {
		if change.Type == "screen" {
			broadcastSSE("screen", map[string]interface{}{
				"screen":  change.Screen,
				"dataUrl": change.DataURL,
			})
		} else {
			broadcastSSE("device", change)
		}
	}

	handler, err := commander.NewBaseLoupeDeckHandler(configFile, commander.HandlerOptions{
		Device:             emulatedDevice,
		Emulate:            true,
		Model:              model,
		DryRun:             dryRun,
		AllowInvalidConfig: true,
		ExitOnStop:         false,
		BaseDir:            baseDir,
		ActivityEmitter: func(entry interfaces.ActivityEntry) {
			broadcastSSE("activity", entry)
		},
	})
	if err != nil {
		return nil, err
	}

	handler.OnStateChange = func() {
		broadcastSSE("snapshot", handler.GetSnapshot())
	}

	if err := handler.Start(); err != nil {
		return nil, err
	}

	buildCombinedValidation := func(overrideProfileIndex *int, overrideProfileData map[string]interface{}) config.ValidationReport {
		fullReport := config.ValidateAppConfigFile(configFile, baseDir)
		if overrideProfileIndex != nil && overrideProfileData != nil && fullReport.ParsedAppConfig != nil {
			idx := *overrideProfileIndex
			fileName := fmt.Sprintf("profile-%d.yaml", idx)
			if profilesRaw, ok := fullReport.ParsedAppConfig["profiles"].([]interface{}); ok && idx >= 0 && idx < len(profilesRaw) {
				if pm, ok := profilesRaw[idx].(map[string]interface{}); ok {
					if fStr := config.ToString(pm["file"]); fStr != "" {
						fileName = fStr
					}
				}
			}
			var otherIssues []config.ValidationIssue
			for _, iss := range fullReport.Issues {
				if iss.File != fileName {
					otherIssues = append(otherIssues, iss)
				}
			}
			appParams, _ := fullReport.ParsedAppConfig["parameters"].(map[string]interface{})
			profCount := 1
			if profilesRaw, ok := fullReport.ParsedAppConfig["profiles"].([]interface{}); ok && len(profilesRaw) > 0 {
				profCount = len(profilesRaw)
			}
			updatedIssues := config.ValidateProfileData(overrideProfileData, config.ValidateOptions{
				FileName:      fileName,
				BaseDir:       baseDir,
				AppParameters: appParams,
				ProfileCount:  profCount,
				ProfileIndex:  idx,
			})
			combined := append(otherIssues, updatedIssues...)
			var errs, warns, infos []config.ValidationIssue
			for _, iss := range combined {
				switch iss.Severity {
				case "error":
					errs = append(errs, iss)
				case "warning":
					warns = append(warns, iss)
				case "info":
					infos = append(infos, iss)
				}
			}
			if errs == nil {
				errs = []config.ValidationIssue{}
			}
			if warns == nil {
				warns = []config.ValidationIssue{}
			}
			if infos == nil {
				infos = []config.ValidationIssue{}
			}
			if combined == nil {
				combined = []config.ValidationIssue{}
			}
			return config.ValidationReport{
				Valid:    len(errs) == 0,
				Errors:   errs,
				Warnings: warns,
				Infos:    infos,
				Issues:   combined,
				Summary: config.ValidationSummary{
					ErrorCount:   len(errs),
					WarningCount: len(warns),
					InfoCount:    len(infos),
					ProfileCount: len(fullReport.ParsedProfiles),
				},
				ParsedAppConfig: fullReport.ParsedAppConfig,
				ParsedProfiles:  fullReport.ParsedProfiles,
			}
		}
		return fullReport
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		pathname := r.URL.Path

		if r.Method == http.MethodGet && (pathname == "/" || pathname == "/index.html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(emulatorUIHTML)
			return
		}

		if r.Method == http.MethodGet && pathname == "/api/events" {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(": connected\n\n"))
			flusher.Flush()

			clientCh := make(chan string, 64)
			sseMu.Lock()
			sseClients[clientCh] = struct{}{}
			sseMu.Unlock()

			defer func() {
				sseMu.Lock()
				delete(sseClients, clientCh)
				sseMu.Unlock()
			}()

			ctx := r.Context()
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-clientCh:
					_, err := w.Write([]byte(msg))
					if err != nil {
						return
					}
					flusher.Flush()
				}
			}
		}

		if r.Method == http.MethodGet && pathname == "/api/state" {
			validation := buildCombinedValidation(nil, nil)
			profilesData := validation.ParsedProfiles
			if profilesData == nil {
				profilesData = []config.ParsedProfileEntry{}
			}
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"snapshot":       handler.GetSnapshot(),
				"validation":     validation,
				"profilesData":   profilesData,
				"availableIcons": listAvailableIcons(baseDir),
				"dryRun":         handler.Ifaces.GetSimulationMode(),
			})
			return
		}

		if r.Method == http.MethodPost && pathname == "/api/event" {
			body, err := readJSONBody(r)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			evtType := config.ToString(body["type"])
			screen := config.ToString(body["screen"])
			if screen == "" {
				screen = "center"
			}
			key := config.ToIntDefault(body["key"], 0)
			x := config.ToFloatDefault(body["x"], 45)
			y := config.ToFloatDefault(body["y"], 45)

			switch evtType {
			case "touchstart":
				emulatedDevice.TriggerTouchStart(screen, key, x, y)
				handler.UpdateScreens(true)
			case "touchmove":
				emulatedDevice.TriggerTouchMove(screen, key, x, y)
				handler.UpdateScreens(true)
			case "touchend":
				emulatedDevice.TriggerTouchEnd(screen, key, x, y)
				handler.UpdateScreens(true)
			case "down":
				emulatedDevice.TriggerButtonDown(body["id"])
			case "up":
				emulatedDevice.TriggerButtonUp(body["id"])
			case "rotate":
				emulatedDevice.TriggerRotate(config.ToString(body["id"]), config.ToIntDefault(body["delta"], 1))
			case "profile":
				_ = handler.ActivateProfile(config.ToIntDefault(body["profileId"], 0))
				handler.UpdateScreens(true)
			case "mode":
				dry, _ := body["dryRun"].(bool)
				handler.Ifaces.SetSimulationMode(dry)
			case "model":
				if mStr := config.ToString(body["model"]); mStr != "" {
					_ = handler.SetEmulatedModel(mStr)
					if handler.EmulatedDevice != nil {
						emulatedDevice = handler.EmulatedDevice
						emulatedDevice.OnStateChange = func(change device.StateChangeEvent) {
							if change.Type == "screen" {
								broadcastSSE("screen", map[string]interface{}{
									"screen":  change.Screen,
									"dataUrl": change.DataURL,
								})
							} else {
								broadcastSSE("device", change)
							}
						}
					}
				}
			}

			handler.UpdateScreens(false)
			snap := handler.GetSnapshot()
			broadcastSSE("snapshot", snap)
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"ok":       true,
				"dryRun":   handler.Ifaces.GetSimulationMode(),
				"snapshot": snap,
			})
			return
		}

		if r.Method == http.MethodPost && pathname == "/api/opcua/simulate" {
			body, err := readJSONBody(r)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			nodeID := config.ToString(body["nodeId"])
			val := body["value"]
			handler.Ifaces.OPCUA.SimulateNodeValue(nodeID, val)
			handler.UpdateScreens(true)
			snap := handler.GetSnapshot()
			broadcastSSE("snapshot", snap)
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"ok":       true,
				"snapshot": snap,
			})
			return
		}

		if r.Method == http.MethodPost && pathname == "/api/validate" {
			body, err := readJSONBody(r)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			var profIdx *int
			if rawIdx, ok := body["profileIndex"]; ok && rawIdx != nil {
				idx := config.ToIntDefault(rawIdx, 0)
				profIdx = &idx
			}
			profData, _ := body["profileData"].(map[string]interface{})
			validation := buildCombinedValidation(profIdx, profData)
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"ok":         true,
				"validation": validation,
			})
			return
		}

		if r.Method == http.MethodPost && pathname == "/api/config/autofix" {
			body, err := readJSONBody(r)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			idx := config.ToIntDefault(body["profileIndex"], 0)
			profData, _ := body["profileData"].(map[string]interface{})
			res := config.AutoFixProfileData(profData)
			validation := buildCombinedValidation(&idx, res.FixedProfileData)
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"ok":               true,
				"fixedProfileData": res.FixedProfileData,
				"fixesApplied":     res.FixesApplied,
				"validation":       validation,
			})
			return
		}

		if r.Method == http.MethodPost && pathname == "/api/config/yaml" {
			body, err := readJSONBody(r)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			profData := body["profileData"]
			if profData == nil {
				profData = map[string]interface{}{}
			}
			yamlStr, err := config.MarshalYAMLString(profData)
			if err != nil {
				sendJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"ok":   true,
				"yaml": yamlStr,
			})
			return
		}

		if r.Method == http.MethodPost && pathname == "/api/config/save" {
			body, err := readJSONBody(r)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
				return
			}
			idx := config.ToIntDefault(body["profileIndex"], 0)
			appParsed, _ := config.ParseConfigFileSafe(cfgPath)
			var fileRel string
			if appParsed != nil {
				if profsRaw, ok := appParsed["profiles"].([]interface{}); ok && idx >= 0 && idx < len(profsRaw) {
					if pm, ok := profsRaw[idx].(map[string]interface{}); ok {
						fileRel = config.ToString(pm["file"])
					}
				}
			}
			if fileRel == "" {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{
					"ok":      false,
					"message": fmt.Sprintf("Profile index %d not found in %s", idx, configFile),
				})
				return
			}

			var profileObj map[string]interface{}
			if rawYAML, ok := body["rawYaml"].(string); ok && rawYAML != "" {
				if err := yaml.Unmarshal([]byte(rawYAML), &profileObj); err != nil {
					sendJSON(w, http.StatusBadRequest, map[string]interface{}{
						"ok":      false,
						"message": fmt.Sprintf("YAML parse error: %v", err),
					})
					return
				}
			} else if pd, ok := body["profileData"].(map[string]interface{}); ok {
				profileObj = pd
			}

			targetPath := filepath.Clean(filepath.Join(baseDir, fileRel))
			if !strings.HasPrefix(targetPath, baseDir+string(filepath.Separator)) && targetPath != baseDir {
				sendJSON(w, http.StatusForbidden, map[string]interface{}{
					"ok":      false,
					"message": "Forbidden: profile file path is outside base directory",
				})
				return
			}

			if strings.HasSuffix(targetPath, ".json") || strings.HasSuffix(targetPath, ".json5") {
				_ = config.WriteJSONFile(targetPath, profileObj)
			} else {
				_ = config.WriteYAMLFile(targetPath, profileObj)
			}

			handler.ReloadConfig(configFile)
			validation := buildCombinedValidation(nil, nil)
			snap := handler.GetSnapshot()
			broadcastSSE("snapshot", snap)

			sendJSON(w, http.StatusOK, map[string]interface{}{
				"ok":          true,
				"message":     fmt.Sprintf("Saved %s and reloaded emulator profile", fileRel),
				"profileData": profileObj,
				"snapshot":    snap,
				"validation":  validation,
			})
			return
		}

		sendJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
	})

	addr := fmt.Sprintf("%s:%d", host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = handler.Stop()
		return nil, err
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{
		Handler: mux,
	}
	go func() {
		_ = srv.Serve(ln)
	}()

	displayHost := host
	if displayHost == "0.0.0.0" {
		displayHost = "localhost"
	}
	modeLabel := "Live Execution"
	if handler.Ifaces.GetSimulationMode() {
		modeLabel = "Simulated (Safe Dry-Run)"
	}
	fmt.Printf("\n🎛️  Loupedeck Emulator & Configuration Studio running at: http://%s:%d\n", displayHost, actualPort)
	fmt.Printf("   Config File: %s\n", configFile)
	fmt.Printf("   Interface Mode: %s\n\n", modeLabel)

	return &ServerInstance{
		HTTPServer:     srv,
		Listener:       ln,
		Handler:        handler,
		EmulatedDevice: emulatedDevice,
		Port:           actualPort,
	}, nil
}
