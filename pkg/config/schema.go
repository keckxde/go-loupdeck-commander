package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppConfigDocument defines the strongly-typed schema for the top-level application config file (e.g. config.yaml).
type AppConfigDocument struct {
	Application   string            `yaml:"application"             json:"application"`
	Device        any               `yaml:"device,omitempty"        json:"device,omitempty"`
	PrimaryDevice string            `yaml:"primaryDevice,omitempty" json:"primaryDevice,omitempty"`
	Columns       *int              `yaml:"columns,omitempty"       json:"columns,omitempty"`
	Rows          *int              `yaml:"rows,omitempty"          json:"rows,omitempty"`
	SerialNumber  string            `yaml:"serialNumber,omitempty"  json:"serialNumber,omitempty"`
	Path          string            `yaml:"path,omitempty"          json:"path,omitempty"`
	Layout        *LayoutDocument   `yaml:"layout,omitempty"        json:"layout,omitempty"`
	Parameters    map[string]any    `yaml:"parameters,omitempty"    json:"parameters,omitempty"`
	Profiles      []ProfileRefEntry `yaml:"profiles"                json:"profiles"`
}

// ProfileRefEntry defines a typed profile reference entry inside AppConfigDocument.Profiles.
type ProfileRefEntry struct {
	Name string `yaml:"name" json:"name"`
	File string `yaml:"file" json:"file"`
}

// LayoutDocument defines the strongly-typed device & touch-grid layout block.
type LayoutDocument struct {
	Device       string `yaml:"device,omitempty"       json:"device,omitempty"`
	Model        string `yaml:"model,omitempty"        json:"model,omitempty"`
	Type         string `yaml:"type,omitempty"         json:"type,omitempty"`
	ID           string `yaml:"id,omitempty"           json:"id,omitempty"`
	Name         string `yaml:"name,omitempty"         json:"name,omitempty"`
	ProductID    *int   `yaml:"productId,omitempty"    json:"productId,omitempty"`
	Columns      *int   `yaml:"columns,omitempty"      json:"columns,omitempty"`
	Rows         *int   `yaml:"rows,omitempty"         json:"rows,omitempty"`
	SerialNumber string `yaml:"serialNumber,omitempty" json:"serialNumber,omitempty"`
	Path         string `yaml:"path,omitempty"         json:"path,omitempty"`
}

// ProfileDocument defines the strongly-typed schema for a profile file (e.g. profile-lights.yaml).
type ProfileDocument struct {
	Name          string                  `yaml:"name,omitempty"          json:"name,omitempty"`
	Profile       string                  `yaml:"profile"                 json:"profile"`
	Description   string                  `yaml:"description,omitempty"   json:"description,omitempty"`
	Device        any                     `yaml:"device,omitempty"        json:"device,omitempty"`
	PrimaryDevice string                  `yaml:"primaryDevice,omitempty" json:"primaryDevice,omitempty"`
	Columns       *int                    `yaml:"columns,omitempty"       json:"columns,omitempty"`
	Rows          *int                    `yaml:"rows,omitempty"          json:"rows,omitempty"`
	SerialNumber  string                  `yaml:"serialNumber,omitempty"  json:"serialNumber,omitempty"`
	Path          string                  `yaml:"path,omitempty"          json:"path,omitempty"`
	Layout        *LayoutDocument         `yaml:"layout,omitempty"        json:"layout,omitempty"`
	Parameters    map[string]any          `yaml:"parameters"              json:"parameters"`
	Default       *ControlParamsSpec      `yaml:"default,omitempty"       json:"default,omitempty"`
	Touch         *TouchDocument          `yaml:"touch"                   json:"touch"`
	Buttons       map[string]*ControlSpec `yaml:"buttons,omitempty"       json:"buttons,omitempty"`
	Knobs         map[string]*ControlSpec `yaml:"knobs,omitempty"         json:"knobs,omitempty"`
}

// TouchDocument defines the strongly-typed touch display sections (center grid, left & right strips).
type TouchDocument struct {
	Center map[string]*ControlSpec `yaml:"center,omitempty" json:"center,omitempty"`
	Left   map[string]*ControlSpec `yaml:"left,omitempty"   json:"left,omitempty"`
	Right  map[string]*ControlSpec `yaml:"right,omitempty"  json:"right,omitempty"`
}

// ControlSpec defines a single touch tile, physical button, or rotary knob configuration.
type ControlSpec struct {
	States OrderedStatesMap   `yaml:"states"                   json:"states"`
	Params *ControlParamsSpec `yaml:"params,omitempty"         json:"params,omitempty"`

	// Legacy top-level properties (supported for backward compatibility & flagged by validator)
	Text           *string  `yaml:"text,omitempty"           json:"text,omitempty"`
	NodeID         *string  `yaml:"nodeid,omitempty"         json:"nodeid,omitempty"`
	Type           *string  `yaml:"type,omitempty"           json:"type,omitempty"`
	Default        any      `yaml:"default,omitempty"        json:"default,omitempty"`
	Group          *string  `yaml:"group,omitempty"          json:"group,omitempty"`
	MinPressed     *float64 `yaml:"minPressed,omitempty"     json:"minPressed,omitempty"`
	MinPressedMode *string  `yaml:"minPressedMode,omitempty" json:"minPressedMode,omitempty"`
}

// OrderedStatesMap preserves YAML key declaration order while decoding each state into a typed ControlParamsSpec.
type OrderedStatesMap struct {
	Keys   []string
	Values map[string]*ControlParamsSpec
}

// UnmarshalYAML decodes a YAML mapping node into OrderedStatesMap while preserving key order.
func (o *OrderedStatesMap) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: states must be a mapping/object", value.Line)
	}
	o.Keys = make([]string, 0, len(value.Content)/2)
	o.Values = make(map[string]*ControlParamsSpec, len(value.Content)/2)
	for i := 0; i+1 < len(value.Content); i += 2 {
		kNode := value.Content[i]
		vNode := value.Content[i+1]
		stateKey := kNode.Value
		var spec ControlParamsSpec
		if vNode.Kind == yaml.MappingNode {
			if err := vNode.Decode(&spec); err != nil {
				return fmt.Errorf("state %q (line %d): %w", stateKey, vNode.Line, err)
			}
		} else if vNode.Kind != yaml.ScalarNode || vNode.Tag != "!!null" {
			return fmt.Errorf("state %q (line %d): state definition must be a mapping/object", stateKey, vNode.Line)
		}
		o.Keys = append(o.Keys, stateKey)
		o.Values[stateKey] = &spec
	}
	return nil
}

// ControlParamsSpec defines the typed attributes valid inside `params`, `states.<key>`, or `default`.
type ControlParamsSpec struct {
	Color          string         `yaml:"color,omitempty"          json:"color,omitempty"`
	TextColor      string         `yaml:"textColor,omitempty"      json:"textColor,omitempty"`
	LegacyTextCol  string         `yaml:"textcolor,omitempty"      json:"textcolor,omitempty"`
	Text           string         `yaml:"text,omitempty"           json:"text,omitempty"`
	TextAlign      string         `yaml:"textAlign,omitempty"      json:"textAlign,omitempty"`
	TextBaseline   string         `yaml:"textBaseline,omitempty"   json:"textBaseline,omitempty"`
	Font           string         `yaml:"font,omitempty"           json:"font,omitempty"`
	Image          string         `yaml:"image,omitempty"          json:"image,omitempty"`
	Value          any            `yaml:"value,omitempty"          json:"value,omitempty"`
	TouchValue     *float64       `yaml:"touchvalue,omitempty"     json:"touchvalue,omitempty"`
	Min            *float64       `yaml:"min,omitempty"            json:"min,omitempty"`
	Max            *float64       `yaml:"max,omitempty"            json:"max,omitempty"`
	NodeID         string         `yaml:"nodeid,omitempty"         json:"nodeid,omitempty"`
	OPCUA          string         `yaml:"opcua,omitempty"          json:"opcua,omitempty"`
	HTTP           string         `yaml:"http,omitempty"           json:"http,omitempty"`
	Cmd            string         `yaml:"cmd,omitempty"            json:"cmd,omitempty"`
	Type           string         `yaml:"type,omitempty"           json:"type,omitempty"`
	Group          string         `yaml:"group,omitempty"          json:"group,omitempty"`
	Filter         string         `yaml:"filter,omitempty"         json:"filter,omitempty"`
	Default        any            `yaml:"default,omitempty"        json:"default,omitempty"`
	Blink          any            `yaml:"blink,omitempty"          json:"blink,omitempty"`
	Vibrate        any            `yaml:"vibrate,omitempty"        json:"vibrate,omitempty"`
	Profile        any            `yaml:"profile,omitempty"        json:"profile,omitempty"`
	Brightness     *float64       `yaml:"brightness,omitempty"     json:"brightness,omitempty"`
	MinPressed     *float64       `yaml:"minPressed,omitempty"     json:"minPressed,omitempty"`
	MinPressedMode string         `yaml:"minPressedMode,omitempty" json:"minPressedMode,omitempty"`
	EndpointURL    string         `yaml:"endpointurl,omitempty"    json:"endpointurl,omitempty"`
	Namespace      any            `yaml:"namespace,omitempty"      json:"namespace,omitempty"`
	Extra          map[string]any `yaml:",inline"                  json:"-"`
}

// ToMap converts ControlParamsSpec into a normalized map[string]any for runtime execution.
func (c *ControlParamsSpec) ToMap() map[string]any {
	out := make(map[string]any)
	if c == nil {
		return out
	}
	for k, v := range c.Extra {
		out[k] = v
	}
	if c.Color != "" {
		out["color"] = c.Color
	}
	if c.LegacyTextCol != "" && c.TextColor == "" {
		out["textcolor"] = c.LegacyTextCol
	}
	if c.TextColor != "" {
		out["textColor"] = c.TextColor
	}
	if c.Text != "" {
		out["text"] = c.Text
	}
	if c.TextAlign != "" {
		out["textAlign"] = c.TextAlign
	}
	if c.TextBaseline != "" {
		out["textBaseline"] = c.TextBaseline
	}
	if c.Font != "" {
		out["font"] = c.Font
	}
	if c.Image != "" {
		out["image"] = c.Image
	}
	if c.Value != nil {
		out["value"] = c.Value
	}
	if c.TouchValue != nil {
		out["touchvalue"] = *c.TouchValue
	}
	if c.Min != nil {
		out["min"] = *c.Min
	}
	if c.Max != nil {
		out["max"] = *c.Max
	}
	if c.NodeID != "" {
		out["nodeid"] = c.NodeID
	}
	if c.OPCUA != "" {
		out["opcua"] = c.OPCUA
	}
	if c.HTTP != "" {
		out["http"] = c.HTTP
	}
	if c.Cmd != "" {
		out["cmd"] = c.Cmd
	}
	if c.Type != "" {
		out["type"] = c.Type
	}
	if c.Group != "" {
		out["group"] = c.Group
	}
	if c.Filter != "" {
		out["filter"] = c.Filter
	}
	if c.Default != nil {
		out["default"] = c.Default
	}
	if c.Blink != nil {
		out["blink"] = c.Blink
	}
	if c.Vibrate != nil {
		out["vibrate"] = c.Vibrate
	}
	if c.Profile != nil {
		out["profile"] = c.Profile
	}
	if c.Brightness != nil {
		out["brightness"] = *c.Brightness
	}
	if c.MinPressed != nil {
		out["minPressed"] = *c.MinPressed
	}
	if c.MinPressedMode != "" {
		out["minPressedMode"] = c.MinPressedMode
	}
	if c.EndpointURL != "" {
		out["endpointurl"] = c.EndpointURL
	}
	if c.Namespace != nil {
		out["namespace"] = c.Namespace
	}
	return out
}

// ToMap converts ControlSpec into the map[string]any representation expected by ButtonField/Button.
func (cs *ControlSpec) ToMap() map[string]any {
	if cs == nil {
		return map[string]any{}
	}
	out := make(map[string]any)
	statesMap := make(map[string]any, len(cs.States.Keys)+1)
	for _, k := range cs.States.Keys {
		statesMap[k] = cs.States.Values[k].ToMap()
	}
	if len(cs.States.Keys) > 0 {
		statesMap["__keys__"] = append([]string(nil), cs.States.Keys...)
	}
	out["states"] = statesMap

	if cs.Params != nil {
		out["params"] = cs.Params.ToMap()
	}
	if cs.Text != nil {
		out["text"] = *cs.Text
	}
	if cs.NodeID != nil {
		out["nodeid"] = *cs.NodeID
	}
	if cs.Type != nil {
		out["type"] = *cs.Type
	}
	if cs.Default != nil {
		out["default"] = cs.Default
	}
	if cs.Group != nil {
		out["group"] = *cs.Group
	}
	if cs.MinPressed != nil {
		out["minPressed"] = *cs.MinPressed
	}
	if cs.MinPressedMode != nil {
		out["minPressedMode"] = *cs.MinPressedMode
	}
	return out
}

func controlsMapToUntyped(src map[string]*ControlSpec) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src)+1)
	keys := make([]string, 0, len(src))
	for k, spec := range src {
		keys = append(keys, k)
		out[k] = spec.ToMap()
	}
	sortKeysJSStyle(keys)
	out["__keys__"] = keys
	return out
}

// ToMap converts TouchDocument into a map[string]any representation for NewTouchConfig.
func (td *TouchDocument) ToMap() map[string]any {
	if td == nil {
		return nil
	}
	out := make(map[string]any)
	if td.Center != nil {
		out["center"] = controlsMapToUntyped(td.Center)
	}
	if td.Left != nil {
		out["left"] = controlsMapToUntyped(td.Left)
	}
	if td.Right != nil {
		out["right"] = controlsMapToUntyped(td.Right)
	}
	return out
}

// NormalizeLayout resolves a ProfileDocument's layout overrides on top of baseLayout.
func (doc *ProfileDocument) NormalizeLayout(baseLayout LayoutConfig) LayoutConfig {
	if doc == nil {
		return baseLayout
	}
	hasOverride := doc.Device != nil || doc.PrimaryDevice != "" || doc.Layout != nil || doc.Columns != nil || doc.Rows != nil
	if !hasOverride {
		return baseLayout
	}
	var rawLay map[string]any
	if doc.Layout != nil {
		rawLay = doc.Layout.ToRawMap()
	}
	var colsVal any
	if doc.Columns != nil {
		colsVal = *doc.Columns
	}
	var rowsVal any
	if doc.Rows != nil {
		rowsVal = *doc.Rows
	}
	merged := map[string]any{
		"device":       firstNonNil(doc.Device, doc.PrimaryDevice, baseLayout.Device),
		"columns":      firstNonNil(mapVal(rawLay, "columns"), colsVal, baseLayout.Columns),
		"rows":         firstNonNil(mapVal(rawLay, "rows"), rowsVal, baseLayout.Rows),
		"serialNumber": firstNonNil(doc.SerialNumber, mapVal(rawLay, "serialNumber"), baseLayout.SerialNumber),
		"path":         firstNonNil(doc.Path, mapVal(rawLay, "path"), baseLayout.Path),
	}
	if rawLay != nil {
		merged["layout"] = rawLay
	}
	return NormalizeLayoutConfig(merged)
}

// ToRawMap converts LayoutDocument to a map[string]any for NormalizeLayoutConfig.
func (l *LayoutDocument) ToRawMap() map[string]any {
	if l == nil {
		return nil
	}
	m := make(map[string]any)
	if l.Device != "" {
		m["device"] = l.Device
	}
	if l.Model != "" {
		m["model"] = l.Model
	}
	if l.Type != "" {
		m["type"] = l.Type
	}
	if l.ID != "" {
		m["id"] = l.ID
	}
	if l.Name != "" {
		m["name"] = l.Name
	}
	if l.ProductID != nil {
		m["productId"] = *l.ProductID
	}
	if l.Columns != nil {
		m["columns"] = *l.Columns
	}
	if l.Rows != nil {
		m["rows"] = *l.Rows
	}
	if l.SerialNumber != "" {
		m["serialNumber"] = l.SerialNumber
	}
	if l.Path != "" {
		m["path"] = l.Path
	}
	return m
}

// Validate checks structural and semantic validity of AppConfigDocument.
func (doc *AppConfigDocument) Validate() error {
	if doc == nil {
		return fmt.Errorf("application config document is nil")
	}
	if strings.TrimSpace(doc.Application) == "" {
		return fmt.Errorf("missing or empty required field 'application'")
	}
	if len(doc.Profiles) == 0 {
		return fmt.Errorf("missing or empty required field 'profiles' (at least 1 profile required)")
	}
	seen := make(map[string]bool, len(doc.Profiles))
	for i, p := range doc.Profiles {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("profiles[%d]: missing or empty 'name'", i)
		}
		if strings.TrimSpace(p.File) == "" {
			return fmt.Errorf("profiles[%d] (%s): missing or empty 'file'", i, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("profiles[%d]: duplicate profile name %q", i, p.Name)
		}
		seen[p.Name] = true
	}
	if doc.Columns != nil && *doc.Columns <= 0 {
		return fmt.Errorf("columns must be > 0 (got %d)", *doc.Columns)
	}
	if doc.Rows != nil && *doc.Rows <= 0 {
		return fmt.Errorf("rows must be > 0 (got %d)", *doc.Rows)
	}
	if doc.Layout != nil {
		if doc.Layout.Columns != nil && *doc.Layout.Columns <= 0 {
			return fmt.Errorf("layout.columns must be > 0 (got %d)", *doc.Layout.Columns)
		}
		if doc.Layout.Rows != nil && *doc.Layout.Rows <= 0 {
			return fmt.Errorf("layout.rows must be > 0 (got %d)", *doc.Layout.Rows)
		}
	}
	return nil
}

// NormalizeLayout resolves the typed AppConfigDocument layout settings into a LayoutConfig.
func (doc *AppConfigDocument) NormalizeLayout() LayoutConfig {
	raw := map[string]any{
		"device":        doc.Device,
		"primaryDevice": doc.PrimaryDevice,
		"serialNumber":  doc.SerialNumber,
		"path":          doc.Path,
	}
	if doc.Columns != nil {
		raw["columns"] = *doc.Columns
	}
	if doc.Rows != nil {
		raw["rows"] = *doc.Rows
	}
	if doc.Layout != nil {
		raw["layout"] = doc.Layout.ToRawMap()
	}
	return NormalizeLayoutConfig(raw)
}

// Validate checks structural and semantic validity of ProfileDocument.
func (doc *ProfileDocument) Validate() error {
	if doc == nil {
		return fmt.Errorf("profile document is nil")
	}
	if strings.TrimSpace(doc.Profile) == "" {
		return fmt.Errorf("missing or empty required field 'profile'")
	}
	if doc.Touch == nil {
		return fmt.Errorf("missing required 'touch' section")
	}
	if doc.Parameters == nil {
		return fmt.Errorf("missing required 'parameters' section")
	}
	if doc.Columns != nil && *doc.Columns <= 0 {
		return fmt.Errorf("columns must be > 0 (got %d)", *doc.Columns)
	}
	if doc.Rows != nil && *doc.Rows <= 0 {
		return fmt.Errorf("rows must be > 0 (got %d)", *doc.Rows)
	}
	if doc.Layout != nil {
		if doc.Layout.Columns != nil && *doc.Layout.Columns <= 0 {
			return fmt.Errorf("layout.columns must be > 0 (got %d)", *doc.Layout.Columns)
		}
		if doc.Layout.Rows != nil && *doc.Layout.Rows <= 0 {
			return fmt.Errorf("layout.rows must be > 0 (got %d)", *doc.Layout.Rows)
		}
	}

	validateControls := func(sectionName string, controls map[string]*ControlSpec) error {
		for key, ctrl := range controls {
			if ctrl == nil {
				return fmt.Errorf("%s.%s: control definition is empty", sectionName, key)
			}
			if len(ctrl.States.Keys) == 0 {
				return fmt.Errorf("%s.%s: must define at least one state in 'states'", sectionName, key)
			}
			if ctrl.Params != nil {
				if err := ctrl.Params.Validate(fmt.Sprintf("%s.%s.params", sectionName, key)); err != nil {
					return err
				}
			}
			for _, stKey := range ctrl.States.Keys {
				st := ctrl.States.Values[stKey]
				if st != nil {
					if err := st.Validate(fmt.Sprintf("%s.%s.states.%s", sectionName, key, stKey)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	if err := validateControls("touch.center", doc.Touch.Center); err != nil {
		return err
	}
	if err := validateControls("touch.left", doc.Touch.Left); err != nil {
		return err
	}
	if err := validateControls("touch.right", doc.Touch.Right); err != nil {
		return err
	}
	if err := validateControls("buttons", doc.Buttons); err != nil {
		return err
	}
	if err := validateControls("knobs", doc.Knobs); err != nil {
		return err
	}
	return nil
}

// Validate checks typed parameter constraints (colors, enums, numeric ranges).
func (p *ControlParamsSpec) Validate(path string) error {
	if p == nil {
		return nil
	}
	if p.Color != "" && !hexColorRegex.MatchString(p.Color) {
		return fmt.Errorf("%s.color: invalid hex color %q (expected #RRGGBB)", path, p.Color)
	}
	if p.TextColor != "" && !hexColorRegex.MatchString(p.TextColor) {
		return fmt.Errorf("%s.textColor: invalid hex color %q (expected #RRGGBB)", path, p.TextColor)
	}
	if p.Type != "" && !sliceContains(validButtonTypes, p.Type) {
		return fmt.Errorf("%s.type: invalid button type %q (allowed: TOGGLE, PUSH)", path, p.Type)
	}
	if p.Filter != "" && !sliceContains(validFilters, p.Filter) {
		return fmt.Errorf("%s.filter: invalid event filter %q (allowed: pressed, released, rotated, touchmove)", path, p.Filter)
	}
	if p.TextAlign != "" && !sliceContains(validTextAligns, p.TextAlign) {
		return fmt.Errorf("%s.textAlign: invalid text alignment %q (allowed: left, center, right)", path, p.TextAlign)
	}
	if p.TextBaseline != "" && !sliceContains(validTextBaselines, p.TextBaseline) {
		return fmt.Errorf("%s.textBaseline: invalid text baseline %q (allowed: top, middle, bottom)", path, p.TextBaseline)
	}
	if p.MinPressedMode != "" && !sliceContains(validMinPressedModes, p.MinPressedMode) {
		return fmt.Errorf("%s.minPressedMode: invalid mode %q (allowed: immediate, delayed)", path, p.MinPressedMode)
	}
	if p.MinPressed != nil && *p.MinPressed < 0 {
		return fmt.Errorf("%s.minPressed: must be >= 0 (got %v)", path, *p.MinPressed)
	}
	if p.Brightness != nil && (*p.Brightness < 0 || *p.Brightness > 100) {
		return fmt.Errorf("%s.brightness: must be between 0 and 1 (or 0..100%%), got %v", path, *p.Brightness)
	}
	if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
		return fmt.Errorf("%s: min (%v) cannot be greater than max (%v)", path, *p.Min, *p.Max)
	}
	return nil
}

// ParseTypedAppConfigFile strictly decodes and validates an AppConfigDocument from a YAML/JSON/JSON5 file.
func ParseTypedAppConfigFile(filePath string) (*AppConfigDocument, error) {
	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	content := string(rawBytes)
	if strings.HasSuffix(strings.ToLower(filePath), ".json5") || strings.HasSuffix(strings.ToLower(filePath), ".json") {
		content = stripJSON5Comments(content)
	}
	return ParseTypedAppConfigBytes([]byte(content))
}

// ParseTypedAppConfigBytes strictly decodes and validates an AppConfigDocument from YAML/JSON bytes.
func ParseTypedAppConfigBytes(data []byte) (*AppConfigDocument, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("configuration file is empty")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc AppConfigDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("schema decode error: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}
	return &doc, nil
}

// ParseTypedProfileFile strictly decodes and validates a ProfileDocument from a YAML/JSON/JSON5 file.
func ParseTypedProfileFile(filePath string) (*ProfileDocument, error) {
	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	content := string(rawBytes)
	if strings.HasSuffix(strings.ToLower(filePath), ".json5") || strings.HasSuffix(strings.ToLower(filePath), ".json") {
		content = stripJSON5Comments(content)
	}
	return ParseTypedProfileBytes([]byte(content))
}

// ParseTypedProfileBytes strictly decodes and validates a ProfileDocument from YAML/JSON bytes.
func ParseTypedProfileBytes(data []byte) (*ProfileDocument, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("profile file is empty")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc ProfileDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("schema decode error: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}
	return &doc, nil
}

func resolveBaseDirFromConfigPath(targetFile string, currentBaseDir string) string {
	if currentBaseDir != "" {
		return currentBaseDir
	}
	absTarget, err := filepath.Abs(targetFile)
	if err != nil {
		absTarget = targetFile
	}
	dir := filepath.Dir(absTarget)
	if filepath.Base(dir) == "config" {
		return filepath.Dir(dir)
	}
	return dir
}
