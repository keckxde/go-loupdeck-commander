package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	placeholderRegex = regexp.MustCompile(`\{([0-9a-zA-Z_]+)\}`)
	hexColorRegex    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// FormatTemplate formats tpl by substituting {key} placeholders from params
// (matching npm string-template semantics) without applying a value transform.
func FormatTemplate(tpl string, params map[string]any) string {
	return FormatTemplateTransform(tpl, params, nil)
}

// FormatTemplateTransform formats tpl by substituting {key} placeholders with
// their ToString values from params. Double-braced {{key}} sequences unescape to
// literal key, missing or nil keys resolve to empty strings, and non-nil
// transform callbacks post-process each substituted value.
func FormatTemplateTransform(tpl string, params map[string]any, transform func(val, key string) string) string {
	if tpl == "" {
		return ""
	}
	if params == nil {
		params = map[string]any{}
	}
	matches := placeholderRegex.FindAllStringSubmatchIndex(tpl, -1)
	if len(matches) == 0 {
		return tpl
	}

	var sb strings.Builder
	lastIdx := 0
	for _, m := range matches {
		matchStart, matchEnd := m[0], m[1]
		keyStart, keyEnd := m[2], m[3]
		key := tpl[keyStart:keyEnd]

		sb.WriteString(tpl[lastIdx:matchStart])

		// Check escaped {{key}}
		if matchStart > 0 && tpl[matchStart-1] == '{' && matchEnd < len(tpl) && tpl[matchEnd] == '}' {
			sb.WriteString(key)
		} else {
			if val, exists := params[key]; exists && val != nil {
				s := ToString(val)
				if transform != nil {
					s = transform(s, key)
				}
				sb.WriteString(s)
			}
		}
		lastIdx = matchEnd
	}
	sb.WriteString(tpl[lastIdx:])
	return sb.String()
}

// ExtractPlaceholders returns all {name} placeholder identifiers found in str
// in order of appearance.
func ExtractPlaceholders(str string) []string {
	matches := placeholderRegex.FindAllStringSubmatch(str, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			out = append(out, m[1])
		}
	}
	return out
}

// ToString converts an arbitrary value to its string representation matching
// JavaScript String(val) semantics: nil becomes "", exact integer float64 values
// omit trailing decimals, and other values fall back to fmt.Sprintf("%v", v).
func ToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		if val == math.Trunc(val) && !math.IsInf(val, 0) && !math.IsNaN(val) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ToFloat converts numeric values and decimal or 0x/0X-prefixed hexadecimal
// strings into float64, returning (0, false) for nil, NaN, Inf, empty strings,
// or unparseable values.
func ToFloat(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, false
		}
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return 0, false
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			if parsed, err := strconv.ParseInt(s[2:], 16, 64); err == nil {
				return float64(parsed), true
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// ToInt converts an arbitrary value to int via ToFloat, returning (0, false) if
// conversion fails or if the numeric value has a fractional component.
func ToInt(v any) (int, bool) {
	f, ok := ToFloat(v)
	if !ok {
		return 0, false
	}
	if f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

// ToFloatDefault converts v to float64 via ToFloat, returning def if conversion fails.
func ToFloatDefault(v any, def float64) float64 {
	if f, ok := ToFloat(v); ok {
		return f
	}
	return def
}

// ToIntDefault converts v to float64 via ToFloat and truncates it to int,
// returning def if conversion fails.
func ToIntDefault(v any, def int) int {
	if f, ok := ToFloat(v); ok {
		return int(f)
	}
	return def
}

// CalcDelta adds delta to data within the inclusive range [minVal, maxVal],
// wrapping values above maxVal to minVal and values below minVal to maxVal.
func CalcDelta(data, delta, minVal, maxVal float64) float64 {
	res := data + delta
	if res > maxVal {
		res = minVal
	} else if res < minVal {
		res = maxVal
	}
	return res
}

// ColorToRGB parses a 6-digit hex color (#RRGGBB) into [3]int{r, g, b},
// returning [3]int{0, 0, 0} if colorAsHex does not match #RRGGBB.
func ColorToRGB(colorAsHex string) [3]int {
	if !hexColorRegex.MatchString(colorAsHex) {
		return [3]int{0, 0, 0}
	}
	r, _ := strconv.ParseInt(colorAsHex[1:3], 16, 64)
	g, _ := strconv.ParseInt(colorAsHex[3:5], 16, 64)
	b, _ := strconv.ParseInt(colorAsHex[5:7], 16, 64)
	return [3]int{int(r), int(g), int(b)}
}

var rgbaNumsRegex = regexp.MustCompile(`\d+`)

// ColorToRGBBytes parses a #RRGGBB hex string or an rgb(...)/rgba(...) CSS
// color string into (r, g, b) bytes, returning (0, 0, 0) for unrecognized inputs.
func ColorToRGBBytes(colorStr string) (byte, byte, byte) {
	s := strings.TrimSpace(colorStr)
	if hexColorRegex.MatchString(s) {
		rgb := ColorToRGB(s)
		return byte(rgb[0]), byte(rgb[1]), byte(rgb[2])
	}
	if strings.HasPrefix(strings.ToLower(s), "rgb") {
		nums := rgbaNumsRegex.FindAllString(s, 3)
		if len(nums) >= 3 {
			r, _ := strconv.Atoi(nums[0])
			g, _ := strconv.Atoi(nums[1])
			b, _ := strconv.Atoi(nums[2])
			return byte(r), byte(g), byte(b)
		}
	}
	return 0, 0, 0
}

// InvertColor inverts each RGB channel (255 - c) of a #RRGGBB hex color and
// returns the result formatted as lowercase #rrggbb.
func InvertColor(colorAsHex string) string {
	rgb := ColorToRGB(colorAsHex)
	for i := 0; i < 3; i++ {
		v := 255 - rgb[i]
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		rgb[i] = v
	}
	return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
}

// DarkerColor scales each RGB channel of a #RRGGBB hex color by factor
// (clamped to [0, 255]) and returns the result formatted as lowercase #rrggbb.
func DarkerColor(colorAsHex string, factor float64) string {
	rgb := ColorToRGB(colorAsHex)
	for i := 0; i < 3; i++ {
		v := int(math.Round(factor * float64(rgb[i])))
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		rgb[i] = v
	}
	return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
}

// SyncParams mutates node in place by copying any key from defaultNode that is
// not already present in node, and returns node.
func SyncParams(node map[string]any, defaultNode map[string]any) map[string]any {
	if node == nil || defaultNode == nil {
		return node
	}
	for k, v := range defaultNode {
		if _, exists := node[k]; !exists {
			node[k] = v
		}
	}
	return node
}

// CloneMap recursively deep-copies src (including nested map[string]any, []any,
// and []string values), returning nil if src is nil.
func CloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = cloneValue(v)
	}
	return out
}

// cloneValue recursively deep-copies map[string]any, []any, and []string
// values, returning scalar values unchanged.
func cloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return CloneMap(val)
	case []any:
		s := make([]any, len(val))
		for i, elem := range val {
			s[i] = cloneValue(elem)
		}
		return s
	case []string:
		s := make([]string, len(val))
		copy(s, val)
		return s
	default:
		return val
	}
}

// CleanInternalKeys recursively returns a copy of src with all internal
// "__keys__" ordering metadata entries removed from nested maps and slices,
// returning nil if src is nil.
func CleanInternalKeys(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if k == "__keys__" {
			continue
		}
		switch sub := v.(type) {
		case map[string]any:
			out[k] = CleanInternalKeys(sub)
		case []any:
			arr := make([]any, len(sub))
			for i, item := range sub {
				if m, ok := item.(map[string]any); ok {
					arr[i] = CleanInternalKeys(m)
				} else {
					arr[i] = item
				}
			}
			out[k] = arr
		default:
			out[k] = v
		}
	}
	return out
}

// OrderedKeys returns the keys of m in their original YAML/JSON declaration
// order (from "__keys__", appending any newly added keys sorted via
// sortKeysJSStyle), or falls back to sortKeysJSStyle when "__keys__" is absent.
// Returns nil if m is nil.
func OrderedKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	if rawOrder, ok := m["__keys__"]; ok {
		if orderSlice, ok := rawOrder.([]string); ok {
			res := make([]string, 0, len(orderSlice))
			seen := make(map[string]bool, len(orderSlice))
			for _, k := range orderSlice {
				if k == "__keys__" {
					continue
				}
				if _, exists := m[k]; exists {
					res = append(res, k)
					seen[k] = true
				}
			}
			// Append any newly added keys deterministically
			var extra []string
			for k := range m {
				if k != "__keys__" && !seen[k] {
					extra = append(extra, k)
				}
			}
			if len(extra) > 0 {
				sortKeysJSStyle(extra)
				res = append(res, extra...)
			}
			return res
		}
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "__keys__" {
			continue
		}
		keys = append(keys, k)
	}
	sortKeysJSStyle(keys)
	return keys
}

// sortKeysJSStyle sorts keys in place using JavaScript-like deterministic
// ordering: non-negative integer strings ascending first, then standard state
// names ("off", "half", "on"), then remaining strings in lexicographical order.
func sortKeysJSStyle(keys []string) {
	statePriority := map[string]int{
		"off":  1,
		"half": 2,
		"on":   3,
	}
	sort.SliceStable(keys, func(i, j int) bool {
		ki, kj := keys[i], keys[j]
		ni, errI := strconv.Atoi(ki)
		nj, errJ := strconv.Atoi(kj)
		isUintI := errI == nil && ni >= 0 && strconv.Itoa(ni) == ki
		isUintJ := errJ == nil && nj >= 0 && strconv.Itoa(nj) == kj
		if isUintI && isUintJ {
			return ni < nj
		}
		if isUintI != isUintJ {
			return isUintI
		}
		pi, okI := statePriority[ki]
		pj, okJ := statePriority[kj]
		if okI && okJ {
			return pi < pj
		}
		if okI != okJ {
			return okI
		}
		return ki < kj
	})
}

// ParseConfigFileSafe reads and parses a YAML, JSON, or JSON5 configuration file
// into a map[string]any without logging side effects. For .json and .json5
// files, JSON5 comments are stripped before decoding via yaml.Node so mapping
// key order is preserved in "__keys__". Returns an error if the file does not
// exist, fails to parse, is empty, or does not decode to a mapping/object.
func ParseConfigFileSafe(filePath string) (map[string]any, error) {
	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("File not found: %s", filePath)
		}
		return nil, err
	}

	content := string(rawBytes)
	if strings.HasSuffix(strings.ToLower(filePath), ".json5") || strings.HasSuffix(strings.ToLower(filePath), ".json") {
		content = stripJSON5Comments(content)
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal([]byte(content), &rootNode); err != nil {
		return nil, fmt.Errorf("Parse error in %s: %v", filePath, err)
	}

	if rootNode.Kind == 0 || len(rootNode.Content) == 0 {
		return nil, fmt.Errorf("Configuration file is empty or not an object.")
	}

	val := yamlNodeToValue(rootNode.Content[0])
	obj, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Configuration file is empty or not an object.")
	}
	return obj, nil
}

// yamlNodeToValue recursively converts a yaml.Node AST into Go values,
// preserving mapping key declaration order in "__keys__".
func yamlNodeToValue(node *yaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) > 0 {
			return yamlNodeToValue(node.Content[0])
		}
		return nil
	case yaml.MappingNode:
		m := make(map[string]any, len(node.Content)/2+1)
		keys := make([]string, 0, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			kNode := node.Content[i]
			vNode := node.Content[i+1]
			key := kNode.Value
			keys = append(keys, key)
			m[key] = yamlNodeToValue(vNode)
		}
		m["__keys__"] = keys
		return m
	case yaml.SequenceNode:
		s := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			s = append(s, yamlNodeToValue(item))
		}
		return s
	case yaml.ScalarNode:
		var out any
		if err := node.Decode(&out); err == nil {
			return out
		}
		return node.Value
	case yaml.AliasNode:
		if node.Alias != nil {
			return yamlNodeToValue(node.Alias)
		}
		return nil
	default:
		return nil
	}
}

// stripJSON5Comments removes // line comments and /* */ block comments outside
// single- and double-quoted string literals while preserving line breaks inside
// block comments so yaml.v3 can parse JSON5 cleanly with accurate line numbers.
func stripJSON5Comments(input string) string {
	var out strings.Builder
	inString := false
	var stringChar byte
	i := 0
	n := len(input)
	for i < n {
		ch := input[i]
		if inString {
			out.WriteByte(ch)
			if ch == '\\' && i+1 < n {
				i++
				out.WriteByte(input[i])
			} else if ch == stringChar {
				inString = false
			}
			i++
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			stringChar = ch
			out.WriteByte(ch)
			i++
			continue
		}
		if ch == '/' && i+1 < n && input[i+1] == '/' {
			i += 2
			for i < n && input[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < n && input[i+1] == '*' {
			i += 2
			for i+1 < n && !(input[i] == '*' && input[i+1] == '/') {
				if input[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			i += 2
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return out.String()
}

// ReadConfigFile reads a YAML, JSON, or JSON5 config file via
// ParseConfigFileSafe, printing any error to os.Stderr and returning nil on failure.
func ReadConfigFile(fileName string) map[string]any {
	data, err := ParseConfigFileSafe(fileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return nil
	}
	return data
}

// WriteJSONFile strips internal "__keys__" metadata (if obj is map[string]any),
// formats obj as 4-space indented JSON, and writes it to fileName with mode 0644.
func WriteJSONFile(fileName string, obj any) error {
	cleaned := obj
	if m, ok := obj.(map[string]any); ok {
		cleaned = CleanInternalKeys(m)
	}
	data, err := json.MarshalIndent(cleaned, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(fileName, data, 0644)
}

// WriteYAMLFile strips internal "__keys__" metadata (if obj is map[string]any),
// marshals obj to YAML, and writes it to fileName with mode 0644.
func WriteYAMLFile(fileName string, obj any) error {
	cleaned := obj
	if m, ok := obj.(map[string]any); ok {
		cleaned = CleanInternalKeys(m)
	}
	data, err := yaml.Marshal(cleaned)
	if err != nil {
		return err
	}
	return os.WriteFile(fileName, data, 0644)
}

// MarshalYAMLString strips internal "__keys__" metadata (if obj is map[string]any)
// and marshals obj into a YAML-formatted string.
func MarshalYAMLString(obj any) (string, error) {
	cleaned := obj
	if m, ok := obj.(map[string]any); ok {
		cleaned = CleanInternalKeys(m)
	}
	data, err := yaml.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
