// Package config provides configuration loading, strongly-typed YAML/JSON/JSON5
// schema decoding, device and grid layout normalization, template formatting,
// and semantic validation for Loupedeck and Razer Stream Controller profiles.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeviceSpec describes the physical capabilities, USB product IDs, grid
// dimensions, button identifiers, rotary knobs, and touch displays of a
// supported Loupedeck or Razer Stream Controller hardware model.
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

// ResolveDeviceSpec resolves a model string, USB product ID, or device
// attribute map into a matching *DeviceSpec. When input is a map[string]any, it
// checks "productId" first and then falls back to "type", "model", "device",
// "primaryDevice", "id", or "name". String inputs are lowercased with any
// "(emulated)" suffix stripped before lookup in DeviceAliases. Returns nil if
// input is nil or does not match a known device.
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

// LayoutConfig represents the normalized hardware device identity, touch-grid
// dimensions (Columns and Rows), optional device connection filters
// (SerialNumber and Path), and flags indicating whether the device or layout
// were explicitly configured.
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

// NormalizeLayoutConfig extracts and normalizes device and touch-grid layout
// settings from rawConfig. It checks "device", "primaryDevice", and "layout"
// blocks (including nested "serialNumber", "path", "columns", and "rows"),
// falling back to the "loupedeck-live" specification when no model matches, and
// records ExplicitDevice and ExplicitLayout accordingly.
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

// ApplicationConfig holds the top-level application configuration, normalized
// device layout, global parameters, loaded profiles, initialization state,
// base directory for relative path resolution, and the underlying typed Schema.
type ApplicationConfig struct {
	Application string             `json:"application"`
	Device      string             `json:"device"`
	Layout      LayoutConfig       `json:"layout"`
	Profiles    []*Profile         `json:"profiles"`
	Parameters  map[string]any     `json:"parameters"`
	Initialized bool               `json:"initialized"`
	BaseDir     string             `json:"-"`
	Schema      *AppConfigDocument `json:"-"`
}

// NewApplicationConfig creates a default ApplicationConfig targeting a
// 4x3 "Loupedeck Live" device with "undefined" application name and empty
// Profiles and Parameters collections.
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

// LoadFromFile loads, strictly parses, and validates the application
// configuration from fileName (resolved against a.BaseDir when relative),
// updates a.BaseDir if unset, and populates ApplicationConfig and all referenced
// profiles via LoadFromTypedDoc. Returns false if reading, parsing, validation,
// or loading any referenced profile fails.
func (a *ApplicationConfig) LoadFromFile(fileName string) bool {
	fmt.Printf("ApplicationConfig: Loading Config File %s\n", fileName)
	targetFile := fileName
	if a.BaseDir != "" && !filepath.IsAbs(fileName) {
		targetFile = filepath.Join(a.BaseDir, fileName)
	}
	doc, err := ParseTypedAppConfigFile(targetFile)
	if err != nil {
		fmt.Printf("ApplicationConfig: Invalid Config File %s (%v) - breaking here\n", fileName, err)
		return false
	}
	a.BaseDir = resolveBaseDirFromConfigPath(targetFile, a.BaseDir)
	return a.LoadFromTypedDoc(doc)
}

// LoadFromTypedDoc validates doc via doc.Validate(), populates ApplicationConfig
// fields (Schema, Application, Layout, Device, Parameters), and loads each
// referenced profile using NewProfileFromRef while merging global parameters via
// SetParams. Sets a.Initialized to true and returns true on success, or returns
// false if validation or any profile load fails.
func (a *ApplicationConfig) LoadFromTypedDoc(doc *AppConfigDocument) bool {
	if err := doc.Validate(); err != nil {
		fmt.Printf("ApplicationConfig: Typed schema validation failed: %v\n", err)
		return false
	}
	a.Schema = doc
	a.Application = doc.Application
	a.Layout = doc.NormalizeLayout()
	a.Device = a.Layout.Device
	a.Parameters = NewParametersConfig(doc.Parameters)
	a.Profiles = make([]*Profile, 0, len(doc.Profiles))

	for i, ref := range doc.Profiles {
		prof := NewProfileFromRef(ref, len(doc.Profiles), i, &a.Layout, a.BaseDir)
		prof.SetParams(doc.Parameters)
		if prof.Loaded {
			a.Profiles = append(a.Profiles, prof)
		} else {
			return false
		}
	}

	a.Initialized = true
	return true
}

// TouchConfig holds the untyped control maps for the Center touch tile grid
// and the Left and Right touch strips.
type TouchConfig struct {
	Center map[string]any `json:"center"`
	Left   map[string]any `json:"left"`
	Right  map[string]any `json:"right"`
}

// NewTouchConfig creates a TouchConfig populated with default Center, Left, and
// Right sections, clones any overrides present in data, and synchronizes every
// control state's missing attributes against defaultState via SyncParams in
// deterministic OrderedKeys order.
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

// defaultCenterTouchMap constructs the default 12-tile ("0".."11") center touch
// grid with "__keys__" ordering and off/on sample states.
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

// NewButtonConfig returns a cloned copy of data when non-nil, or generates
// default physical button definitions for button "0" (brightness toggle with
// off/half/on states) and buttons "1".."maxIndex" (profile switchers capped at
// button "7", marking index+1 with "default": "on") along with "__keys__" ordering.
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

// NewKnobsConfig returns a cloned copy of data when non-nil, or generates
// default rotary knob configurations for knobTL, knobCL, knobBL, knobTR,
// knobCR, and knobBR with "__keys__" ordering.
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

// NewParametersConfig returns a cloned copy of data when non-nil, or returns
// default host, OPC UA endpoint, node ID, and numeric range parameters.
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

// NewDefaultConfig returns a cloned copy of data when non-nil, or returns the
// default button state attributes ({"color": "#111111"}).
func NewDefaultConfig(data map[string]any) map[string]any {
	if data != nil {
		return CloneMap(data)
	}
	return map[string]any{
		"color": "#111111",
	}
}

// Profile represents a single loaded device profile, including its metadata,
// resolved LayoutConfig, Touch/Knobs/Buttons control maps, profile Parameters,
// Default state attributes, source File/BaseDir, Loaded flag, and typed Schema.
type Profile struct {
	Name        string           `json:"name"`
	Profile     string           `json:"profile"`
	Description string           `json:"description"`
	Device      string           `json:"device"`
	Layout      LayoutConfig     `json:"layout"`
	Touch       TouchConfig      `json:"touch"`
	Knobs       map[string]any   `json:"knobs"`
	Buttons     map[string]any   `json:"buttons"`
	Parameters  map[string]any   `json:"parameters"`
	Default     map[string]any   `json:"default"`
	File        string           `json:"-"`
	BaseDir     string           `json:"-"`
	Loaded      bool             `json:"loaded"`
	Schema      *ProfileDocument `json:"-"`

	profileCount int
	index        int
	appLayout    *LayoutConfig
}

// NewProfileFromRef initializes a Profile from a typed ProfileRefEntry with
// default control maps and immediately loads ref.File from disk using baseDir
// and appLayout. Callers should check p.Loaded to verify loading succeeded.
func NewProfileFromRef(ref ProfileRefEntry, profileCount int, index int, appLayout *LayoutConfig, baseDir string) *Profile {
	p := &Profile{
		Name:         ref.Name,
		File:         ref.File,
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

// NewProfile creates and loads a Profile from a raw map entry containing "name"
// and "file" keys by delegating to NewProfileFromRef.
func NewProfile(data map[string]any, profileCount int, index int, appLayout *LayoutConfig, baseDir string) *Profile {
	return NewProfileFromRef(ProfileRefEntry{
		Name: ToString(data["name"]),
		File: ToString(data["file"]),
	}, profileCount, index, appLayout, baseDir)
}

// LoadFromFile resolves fileName relative to p.BaseDir (when non-absolute and
// present on disk), strictly parses and validates the profile via
// ParseTypedProfileFile, and populates p via LoadFromTypedDoc. Returns false if
// parsing or validation fails.
func (p *Profile) LoadFromFile(fileName string) bool {
	fmt.Printf("ProfileConfig: Loading Profile  File %s, Index %d\n", fileName, p.index)
	targetPath := fileName
	if p.BaseDir != "" && !filepath.IsAbs(fileName) {
		cand := filepath.Join(p.BaseDir, fileName)
		if _, err := os.Stat(cand); err == nil {
			targetPath = cand
		}
	}
	doc, err := ParseTypedProfileFile(targetPath)
	if err != nil {
		fmt.Printf("ProfileConfig: Invalid Profile File %s (%v)\n", fileName, err)
		return false
	}
	if len(doc.Buttons) == 0 {
		fmt.Printf("ProfileConfig: File %s is missing a buttons attribute - will be used to switch profiles by default\n", fileName)
	}
	return p.LoadFromTypedDoc(doc)
}

// LoadFromTypedDoc validates doc via doc.Validate(), resolves the profile's
// effective Layout on top of appLayout (or default layout), populates Schema,
// Profile, Description, Parameters, Default, Touch, Buttons, and Knobs, and
// sets p.Loaded to true. Returns false if doc fails validation.
func (p *Profile) LoadFromTypedDoc(doc *ProfileDocument) bool {
	if err := doc.Validate(); err != nil {
		fmt.Printf("ProfileConfig: Typed schema validation failed: %v\n", err)
		return false
	}
	p.Schema = doc
	p.Profile = doc.Profile
	p.Description = doc.Description

	baseLayout := NormalizeLayoutConfig(nil)
	if p.appLayout != nil {
		baseLayout = *p.appLayout
	}
	p.Layout = doc.NormalizeLayout(baseLayout)
	p.Device = p.Layout.Device

	var rawDefault map[string]any
	if doc.Default != nil {
		rawDefault = doc.Default.ToMap()
	}

	p.Parameters = NewParametersConfig(doc.Parameters)
	p.Default = NewDefaultConfig(rawDefault)
	p.Touch = NewTouchConfig(doc.Touch.ToMap(), p.Default)
	p.Buttons = NewButtonConfig(controlsMapToUntyped(doc.Buttons), p.profileCount, p.index)
	p.Knobs = NewKnobsConfig(controlsMapToUntyped(doc.Knobs))

	p.Loaded = true
	return true
}

// SetParams merges global application parameters into p.Parameters for any key
// not already defined by the profile, skipping internal "__keys__" metadata.
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

// firstNonNil returns the first value in vals that is non-nil and produces a
// non-empty string via ToString, or nil if all values are empty or nil.
func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil && ToString(v) != "" {
			return v
		}
	}
	return nil
}

// mapVal safely looks up key in m, returning nil when m is nil.
func mapVal(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}
