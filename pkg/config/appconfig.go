package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeviceSpec describes the physical capabilities and layout of a supported Loupedeck / Razer model.
type DeviceSpec struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	ProductIDs []uint16 `json:"productIds"`
	Columns    int      `json:"columns"`
	Rows       int      `json:"rows"`
	Buttons    []any    `json:"buttons"`
	Knobs      []string `json:"knobs"`
	Displays   []string `json:"displays"`
}

// DeviceSpecs maps canonical device IDs to their hardware specifications.
var DeviceSpecs = map[string]*DeviceSpec{
	"loupedeck-live": {
		ID:         "loupedeck-live",
		Type:       "Loupedeck Live",
		ProductIDs: []uint16{0x0004},
		Columns:    4,
		Rows:       3,
		Buttons:    []any{0, 1, 2, 3, 4, 5, 6, 7},
		Knobs:      []string{"knobCL", "knobCR", "knobTL", "knobTR", "knobBL", "knobBR"},
		Displays:   []string{"center", "left", "right"},
	},
	"loupedeck-ct": {
		ID:         "loupedeck-ct",
		Type:       "Loupedeck CT",
		ProductIDs: []uint16{0x0003, 0x0007},
		Columns:    4,
		Rows:       3,
		Buttons:    []any{0, 1, 2, 3, 4, 5, 6, 7, "home", "enter", "undo", "save", "keyboard", "fnL", "a", "b", "c", "d", "fnR", "e"},
		Knobs:      []string{"knobCL", "knobCR", "knobTL", "knobTR", "knobBL", "knobBR"},
		Displays:   []string{"center", "left", "right", "knob"},
	},
	"loupedeck-live-s": {
		ID:         "loupedeck-live-s",
		Type:       "Loupedeck Live S",
		ProductIDs: []uint16{0x0006},
		Columns:    5,
		Rows:       3,
		Buttons:    []any{0, 1, 2, 3},
		Knobs:      []string{"knobCL", "knobTL"},
		Displays:   []string{"center"},
	},
	"razer-stream-controller": {
		ID:         "razer-stream-controller",
		Type:       "Razer Stream Controller",
		ProductIDs: []uint16{0x0d06},
		Columns:    4,
		Rows:       3,
		Buttons:    []any{0, 1, 2, 3, 4, 5, 6, 7},
		Knobs:      []string{"knobCL", "knobCR", "knobTL", "knobTR", "knobBL", "knobBR"},
		Displays:   []string{"center", "left", "right"},
	},
	"razer-stream-controller-x": {
		ID:         "razer-stream-controller-x",
		Type:       "Razer Stream Controller X",
		ProductIDs: []uint16{0x0d09},
		Columns:    5,
		Rows:       3,
		Buttons:    []any{},
		Knobs:      []string{},
		Displays:   []string{"center"},
	},
}

// DeviceAliases maps human-friendly model strings to canonical device IDs.
var DeviceAliases = map[string]string{
	"live":                      "loupedeck-live",
	"loupedeck live":            "loupedeck-live",
	"loupedeck-live":            "loupedeck-live",
	"loupedeck_live":            "loupedeck-live",
	"loupedecklive":             "loupedeck-live",
	"ct":                        "loupedeck-ct",
	"ctv2":                      "loupedeck-ct",
	"loupedeck ct":              "loupedeck-ct",
	"loupedeck ctv2":            "loupedeck-ct",
	"loupedeck-ct":              "loupedeck-ct",
	"loupedeck_ct":              "loupedeck-ct",
	"loupedeckct":               "loupedeck-ct",
	"live-s":                    "loupedeck-live-s",
	"lives":                     "loupedeck-live-s",
	"live s":                    "loupedeck-live-s",
	"loupedeck live s":          "loupedeck-live-s",
	"loupedeck-live-s":          "loupedeck-live-s",
	"loupedeck_live_s":          "loupedeck-live-s",
	"loupedecklives":            "loupedeck-live-s",
	"razer":                     "razer-stream-controller",
	"razer stream controller":   "razer-stream-controller",
	"razer-stream-controller":   "razer-stream-controller",
	"razer_stream_controller":   "razer-stream-controller",
	"razerstreamcontroller":     "razer-stream-controller",
	"razer-x":                   "razer-stream-controller-x",
	"razer x":                   "razer-stream-controller-x",
	"razer stream controller x": "razer-stream-controller-x",
	"razer-stream-controller-x": "razer-stream-controller-x",
	"razer_stream_controller_x": "razer-stream-controller-x",
	"razerstreamcontrollerx":    "razer-stream-controller-x",
}

// ResolveDeviceSpec resolves a model string, product ID, or device map into a *DeviceSpec.
func ResolveDeviceSpec(input any) *DeviceSpec {
	if input == nil {
		return nil
	}
	if m, ok := input.(map[string]any); ok {
		if pidVal, hasPid := m["productId"]; hasPid && pidVal != nil {
			if pidInt, ok := ToInt(pidVal); ok {
				for _, spec := range DeviceSpecs {
					for _, p := range spec.ProductIDs {
						if int(p) == pidInt {
							return spec
						}
					}
				}
			}
		}
		for _, k := range []string{"type", "model", "device", "primaryDevice", "id", "name"} {
			if cand, ok := m[k]; ok && cand != nil && ToString(cand) != "" {
				return ResolveDeviceSpec(cand)
			}
		}
		return nil
	}
	str := strings.ToLower(ToString(input))
	str = strings.ReplaceAll(str, "(emulated)", "")
	str = strings.TrimSpace(str)
	if canonicalID, ok := DeviceAliases[str]; ok {
		return DeviceSpecs[canonicalID]
	}
	return nil
}

// LayoutConfig represents the normalized layout & primary device configuration.
type LayoutConfig struct {
	Device         string `json:"device"`
	DeviceID       string `json:"deviceId"`
	Columns        int    `json:"columns"`
	Rows           int    `json:"rows"`
	SerialNumber   string `json:"serialNumber,omitempty"`
	Path           string `json:"path,omitempty"`
	ExplicitDevice bool   `json:"explicitDevice"`
	ExplicitLayout bool   `json:"explicitLayout"`
}

// NormalizeLayoutConfig extracts and normalizes device and grid layout settings from a config map.
func NormalizeLayoutConfig(rawConfig map[string]any) LayoutConfig {
	if rawConfig == nil {
		rawConfig = map[string]any{}
	}
	rawLayout, _ := rawConfig["layout"].(map[string]any)

	var rawDevice any = rawConfig["device"]
	if rawDevice == nil {
		rawDevice = rawConfig["primaryDevice"]
	}
	if rawDevice == nil && rawLayout != nil {
		rawDevice = rawLayout["device"]
		if rawDevice == nil {
			rawDevice = rawLayout["model"]
		}
	}

	deviceInput := rawDevice
	serialNumber := ToString(rawConfig["serialNumber"])
	if serialNumber == "" && rawLayout != nil {
		serialNumber = ToString(rawLayout["serialNumber"])
	}
	path := ToString(rawConfig["path"])
	if path == "" && rawLayout != nil {
		path = ToString(rawLayout["path"])
	}

	if devMap, ok := rawDevice.(map[string]any); ok {
		for _, k := range []string{"type", "model", "name", "id"} {
			if v, exists := devMap[k]; exists && v != nil {
				deviceInput = v
				break
			}
		}
		if v, exists := devMap["serialNumber"]; exists && v != nil {
			serialNumber = ToString(v)
		}
		if v, exists := devMap["path"]; exists && v != nil {
			path = ToString(v)
		}
		if rawLayout == nil {
			rawLayout = devMap
		}
	}

	spec := ResolveDeviceSpec(deviceInput)
	if spec == nil {
		spec = DeviceSpecs["loupedeck-live"]
	}

	columns := spec.Columns
	if rawLayout != nil {
		if c, ok := ToInt(rawLayout["columns"]); ok && c > 0 {
			columns = c
		}
	}
	if c, ok := ToInt(rawConfig["columns"]); ok && c > 0 && (rawLayout == nil || rawLayout["columns"] == nil) {
		columns = c
	}

	rows := spec.Rows
	if rawLayout != nil {
		if r, ok := ToInt(rawLayout["rows"]); ok && r > 0 {
			rows = r
		}
	}
	if r, ok := ToInt(rawConfig["rows"]); ok && r > 0 && (rawLayout == nil || rawLayout["rows"] == nil) {
		rows = r
	}

	_, hasLayout := rawConfig["layout"]
	_, hasCols := rawConfig["columns"]
	_, hasRows := rawConfig["rows"]
	devHasGrid := false
	if devMap, ok := rawDevice.(map[string]any); ok {
		_, c1 := devMap["columns"]
		_, r1 := devMap["rows"]
		devHasGrid = c1 || r1
	}

	return LayoutConfig{
		Device:         spec.Type,
		DeviceID:       spec.ID,
		Columns:        columns,
		Rows:           rows,
		SerialNumber:   serialNumber,
		Path:           path,
		ExplicitDevice: deviceInput != nil && ToString(deviceInput) != "",
		ExplicitLayout: hasLayout || hasCols || hasRows || devHasGrid,
	}
}

// ApplicationConfig holds the top-level application configuration and its loaded profiles.
type ApplicationConfig struct {
	Application string         `json:"application"`
	Device      string         `json:"device"`
	Layout      LayoutConfig   `json:"layout"`
	Profiles    []*Profile     `json:"profiles"`
	Parameters  map[string]any `json:"parameters"`
	Initialized bool           `json:"initialized"`
	BaseDir     string         `json:"-"`
}

// NewApplicationConfig creates a default ApplicationConfig.
func NewApplicationConfig() *ApplicationConfig {
	return &ApplicationConfig{
		Application: "undefined",
		Device:      "Loupedeck Live",
		Layout: LayoutConfig{
			Device:   "Loupedeck Live",
			DeviceID: "loupedeck-live",
			Columns:  4,
			Rows:     3,
		},
		Profiles:   []*Profile{},
		Parameters: map[string]any{},
	}
}

// LoadFromFile loads the application configuration and all its profiles from disk.
func (a *ApplicationConfig) LoadFromFile(fileName string) bool {
	fmt.Printf("ApplicationConfig: Loading Config File %s\n", fileName)
	targetFile := fileName
	if a.BaseDir != "" && !filepath.IsAbs(fileName) {
		targetFile = filepath.Join(a.BaseDir, fileName)
	}
	cfg := ReadConfigFile(targetFile)
	if cfg == nil {
		fmt.Printf("ApplicationConfig: Could not read/parse Config File %s - breaking here\n", fileName)
		return false
	}
	if a.BaseDir == "" && filepath.IsAbs(targetFile) {
		dir := filepath.Dir(targetFile)
		if filepath.Base(dir) == "config" {
			a.BaseDir = filepath.Dir(dir)
		} else {
			a.BaseDir = dir
		}
	}
	appStr := ToString(cfg["application"])
	if appStr == "" {
		fmt.Printf("ApplicationConfig: Config File %s does not contain an application attribute  - breaking here\n", fileName)
		return false
	}
	rawProfiles, hasProfiles := cfg["profiles"].([]any)
	if !hasProfiles {
		fmt.Printf("ApplicationConfig: Config File %s does not contain a profiles attribute  - breaking here\n", fileName)
		return false
	}

	a.Application = appStr
	a.Layout = NormalizeLayoutConfig(cfg)
	a.Device = a.Layout.Device
	rawParams, _ := cfg["parameters"].(map[string]any)
	a.Parameters = NewParametersConfig(rawParams)
	a.Profiles = []*Profile{}

	for i, item := range rawProfiles {
		entryMap, ok := item.(map[string]any)
		if !ok {
			return false
		}
		prof := NewProfile(entryMap, len(rawProfiles), i, &a.Layout, a.BaseDir)
		prof.SetParams(rawParams)
		if prof.Loaded {
			a.Profiles = append(a.Profiles, prof)
		} else {
			return false
		}
	}

	a.Initialized = true
	return true
}

// TouchConfig holds the touch screen configurations (center, left, right).
type TouchConfig struct {
	Center map[string]any `json:"center"`
	Left   map[string]any `json:"left"`
	Right  map[string]any `json:"right"`
}

// NewTouchConfig creates a TouchConfig and synchronizes button states with defaultState.
func NewTouchConfig(data map[string]any, defaultState map[string]any) TouchConfig {
	tc := TouchConfig{
		Center: defaultCenterTouchMap(),
		Left: map[string]any{
			"0": map[string]any{
				"states": map[string]any{
					"on": map[string]any{"color": "#000000", "cmd": "echo \"{id} {state}\""},
				},
				"group": "",
			},
		},
		Right: map[string]any{
			"0": map[string]any{
				"states": map[string]any{
					"on": map[string]any{"color": "#000000", "cmd": "echo \"{id} {state}\""},
				},
				"group": "",
			},
		},
	}

	if data == nil {
		return tc
	}
	if c, ok := data["center"].(map[string]any); ok {
		tc.Center = CloneMap(c)
	}
	if l, ok := data["left"].(map[string]any); ok {
		tc.Left = CloneMap(l)
	}
	if r, ok := data["right"].(map[string]any); ok {
		tc.Right = CloneMap(r)
	}

	for _, section := range []map[string]any{tc.Center, tc.Left, tc.Right} {
		if section == nil {
			continue
		}
		for _, btnKey := range OrderedKeys(section) {
			btnMap, ok := section[btnKey].(map[string]any)
			if !ok || btnMap == nil {
				continue
			}
			statesMap, ok := btnMap["states"].(map[string]any)
			if !ok || statesMap == nil {
				continue
			}
			for _, stateKey := range OrderedKeys(statesMap) {
				stateOld, ok := statesMap[stateKey].(map[string]any)
				if !ok || stateOld == nil {
					continue
				}
				statesMap[stateKey] = SyncParams(CloneMap(stateOld), defaultState)
			}
		}
	}
	return tc
}

func defaultCenterTouchMap() map[string]any {
	m := make(map[string]any, 13)
	keys := make([]string, 12)
	for i := 0; i < 12; i++ {
		k := fmt.Sprintf("%d", i)
		keys[i] = k
		m[k] = map[string]any{
			"states": map[string]any{
				"__keys__": []string{"off", "on"},
				"off":      map[string]any{"color": "#000099", "image": "icons/home.png", "cmd": "echo \"{id} {state}\""},
				"on":       map[string]any{"color": "#00ff00", "image": "icons/home.png", "cmd": "echo \"{id} {state}\""},
			},
		}
	}
	m["__keys__"] = keys
	return m
}

// NewButtonConfig creates a physical buttons config or generates default brightness/profile buttons.
func NewButtonConfig(data map[string]any, profileCount int, index int) map[string]any {
	if data != nil {
		return CloneMap(data)
	}
	buttons := make(map[string]any)
	maxIndex := profileCount
	if maxIndex > 7 {
		maxIndex = 7
	}
	if maxIndex < 0 {
		maxIndex = 0
	}
	keys := make([]string, 0, maxIndex+1)
	for i := 0; i <= maxIndex; i++ {
		k := fmt.Sprintf("%d", i)
		keys = append(keys, k)
		if i == 0 {
			buttons[k] = map[string]any{
				"states": map[string]any{
					"__keys__": []string{"off", "half", "on"},
					"off":      map[string]any{"filter": "pressed", "color": "#005500", "brightness": 0},
					"half":     map[string]any{"filter": "pressed", "brightness": 0.5, "vibrate": 0x58, "color": "#00aa00"},
					"on":       map[string]any{"filter": "pressed", "brightness": 1, "color": "#00ff00"},
				},
				"params": map[string]any{
					"group":          "brightness",
					"default":        "on",
					"filter":         "pressed",
					"minPressed":     0,
					"minPressedMode": "immediate",
				},
			}
		} else {
			profIdx := i - 1
			params := map[string]any{
				"group":          "profiles",
				"profile":        profIdx,
				"filter":         "pressed",
				"minPressed":     0,
				"minPressedMode": "immediate",
			}
			if i == index+1 {
				params["default"] = "on"
			}
			buttons[k] = map[string]any{
				"states": map[string]any{
					"__keys__": []string{"off", "on"},
					"off":      map[string]any{"filter": "pressed", "color": "#000055", "profile": profIdx},
					"on":       map[string]any{"filter": "pressed", "color": "#00ff00", "profile": profIdx},
				},
				"params": params,
			}
		}
	}
	buttons["__keys__"] = keys
	return buttons
}

// NewKnobsConfig creates a knobs config map.
func NewKnobsConfig(data map[string]any) map[string]any {
	if data != nil {
		return CloneMap(data)
	}
	knobIDs := []string{"knobTL", "knobCL", "knobBL", "knobTR", "knobCR", "knobBR"}
	out := make(map[string]any, len(knobIDs)+1)
	for _, k := range knobIDs {
		out[k] = map[string]any{
			"states": map[string]any{
				"__keys__": []string{"on"},
				"on":       map[string]any{"cmd": "echo \"{id} {state}\""},
			},
			"group": "",
		}
	}
	out["__keys__"] = knobIDs
	return out
}

// NewParametersConfig returns parameters or defaults.
func NewParametersConfig(data map[string]any) map[string]any {
	if data != nil {
		return CloneMap(data)
	}
	return map[string]any{
		"hostname":    "127.0.0.1",
		"endpointurl": "opc.tcp://{hostname}:4840",
		"nodeid":      "ns=0;s=nodeID",
		"min":         0,
		"max":         100,
	}
}

// NewDefaultConfig returns default button state config.
func NewDefaultConfig(data map[string]any) map[string]any {
	if data != nil {
		return CloneMap(data)
	}
	return map[string]any{
		"color": "#111111",
	}
}

// Profile represents a single loaded profile configuration.
type Profile struct {
	Name        string         `json:"name"`
	Profile     string         `json:"profile"`
	Description string         `json:"description"`
	Device      string         `json:"device"`
	Layout      LayoutConfig   `json:"layout"`
	Touch       TouchConfig    `json:"touch"`
	Knobs       map[string]any `json:"knobs"`
	Buttons     map[string]any `json:"buttons"`
	Parameters  map[string]any `json:"parameters"`
	Default     map[string]any `json:"default"`
	File        string         `json:"-"`
	BaseDir     string         `json:"-"`
	Loaded      bool           `json:"loaded"`

	profileCount int
	index        int
	appLayout    *LayoutConfig
}

// NewProfile creates and loads a Profile from disk.
func NewProfile(data map[string]any, profileCount int, index int, appLayout *LayoutConfig, baseDir string) *Profile {
	p := &Profile{
		Name:         ToString(data["name"]),
		File:         ToString(data["file"]),
		BaseDir:      baseDir,
		profileCount: profileCount,
		index:        index,
		appLayout:    appLayout,
		Touch:        NewTouchConfig(nil, nil),
		Buttons:      NewButtonConfig(nil, 0, 0),
		Knobs:        NewKnobsConfig(nil),
		Parameters:   NewParametersConfig(nil),
		Default:      NewDefaultConfig(nil),
	}
	p.LoadFromFile(p.File)
	return p
}

// LoadFromFile loads the profile YAML/JSON file.
func (p *Profile) LoadFromFile(fileName string) bool {
	fmt.Printf("ProfileConfig: Loading Profile  File %s, Index %d\n", fileName, p.index)
	targetPath := fileName
	if p.BaseDir != "" && !filepath.IsAbs(fileName) {
		cand := filepath.Join(p.BaseDir, fileName)
		if _, err := os.Stat(cand); err == nil {
			targetPath = cand
		}
	}
	cfg := ReadConfigFile(targetPath)
	if cfg == nil {
		fmt.Printf("ProfileConfig: Cannot parse/load Profile File %s\n", fileName)
		return false
	}
	if ToString(cfg["profile"]) == "" {
		fmt.Printf("ProfileConfig: File %s is missing a profile attribute\n", fileName)
		return false
	}
	if _, ok := cfg["touch"].(map[string]any); !ok {
		fmt.Printf("ProfileConfig: File %s is missing a touch attribute\n", fileName)
		return false
	}
	if _, ok := cfg["parameters"].(map[string]any); !ok {
		fmt.Printf("ProfileConfig: File %s is missing a parameters attribute\n", fileName)
		return false
	}
	if _, ok := cfg["buttons"].(map[string]any); !ok {
		fmt.Printf("ProfileConfig: File %s is missing a buttons attribute - will be used to switch profiles by default\n", fileName)
	}

	p.Profile = ToString(cfg["profile"])
	p.Description = ToString(cfg["description"])

	baseLayout := NormalizeLayoutConfig(nil)
	if p.appLayout != nil {
		baseLayout = *p.appLayout
	}

	_, hasDev := cfg["device"]
	_, hasPrim := cfg["primaryDevice"]
	_, hasLay := cfg["layout"]
	_, hasCols := cfg["columns"]
	_, hasRows := cfg["rows"]
	if hasDev || hasPrim || hasLay || hasCols || hasRows {
		rawLay, _ := cfg["layout"].(map[string]any)
		merged := map[string]any{
			"device":       firstNonNil(cfg["device"], cfg["primaryDevice"], baseLayout.Device),
			"columns":      firstNonNil(mapVal(rawLay, "columns"), cfg["columns"], baseLayout.Columns),
			"rows":         firstNonNil(mapVal(rawLay, "rows"), cfg["rows"], baseLayout.Rows),
			"serialNumber": firstNonNil(cfg["serialNumber"], mapVal(rawLay, "serialNumber"), baseLayout.SerialNumber),
			"path":         firstNonNil(cfg["path"], mapVal(rawLay, "path"), baseLayout.Path),
			"layout":       cfg["layout"],
		}
		p.Layout = NormalizeLayoutConfig(merged)
	} else {
		p.Layout = baseLayout
	}
	p.Device = p.Layout.Device

	rawParams, _ := cfg["parameters"].(map[string]any)
	rawDefault, _ := cfg["default"].(map[string]any)
	rawTouch, _ := cfg["touch"].(map[string]any)
	rawButtons, _ := cfg["buttons"].(map[string]any)
	rawKnobs, _ := cfg["knobs"].(map[string]any)

	p.Parameters = NewParametersConfig(rawParams)
	p.Default = NewDefaultConfig(rawDefault)
	p.Touch = NewTouchConfig(rawTouch, p.Default)
	p.Buttons = NewButtonConfig(rawButtons, p.profileCount, p.index)
	p.Knobs = NewKnobsConfig(rawKnobs)

	p.Loaded = true
	return true
}

// SetParams merges global application parameters into the profile parameters if not overridden.
func (p *Profile) SetParams(parameters map[string]any) {
	if parameters == nil {
		return
	}
	if p.Parameters == nil {
		p.Parameters = map[string]any{}
	}
	for k, v := range parameters {
		if k == "__keys__" {
			continue
		}
		if _, exists := p.Parameters[k]; !exists {
			p.Parameters[k] = v
		}
	}
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil && ToString(v) != "" {
			return v
		}
	}
	return nil
}

func mapVal(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

