package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	validKnobIDs         = []string{"knobTL", "knobCL", "knobBL", "knobTR", "knobCR", "knobBR"}
	validButtonIDs       = []string{"0", "1", "2", "3", "4", "5", "6", "7"}
	validSideIDs         = []string{"0"}
	validButtonTypes     = []string{"", "TOGGLE", "PUSH"}
	validFilters         = []string{"", "pressed", "released", "rotated", "touchmove"}
	validTextAligns      = []string{"left", "center", "right"}
	validTextBaselines   = []string{"top", "middle", "bottom"}
	validMinPressedModes = []string{"immediate", "delayed"}
	nodeIDRegex          = regexp.MustCompile(`^ns=\d+;[sibg]=.+$`)

	builtinButtonParams = []string{
		"id", "key", "state", "width", "height", "min", "max", "x", "y",
		"color", "text", "textColor", "textAlign", "textBaseline", "font",
		"value", "touchvalue", "minPressed", "minPressedMode", "moveLeft", "moveRight", "moveUp", "moveDown",
		"nodeid", "type", "group", "filter", "blink", "vibrate", "profile",
		"brightness", "default", "event", "pressed", "released", "rotated", "image",
	}

	legacyButtonProps = []string{"minPressed", "text", "nodeid", "type", "default", "group"}
)

// ValidationIssue represents a single error, warning, or informational notice
// emitted while validating an application or profile configuration file.
// Severity is one of "error", "warning", or "info"; File and Path identify the
// source location, and Suggestion provides optional remediation guidance.
type ValidationIssue struct {
	Severity   string `json:"severity"`
	File       string `json:"file"`
	Path       string `json:"path"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ValidationSummary summarizes issue counts across all validated configuration
// files along with the total number of parsed profiles.
type ValidationSummary struct {
	ErrorCount   int `json:"errorCount"`
	WarningCount int `json:"warningCount"`
	InfoCount    int `json:"infoCount"`
	ProfileCount int `json:"profileCount"`
}

// ParsedProfileEntry holds a parsed profile reference and its cleaned untyped map
// (with internal "__keys__" metadata stripped) for inclusion in ValidationReport.
type ParsedProfileEntry struct {
	Index int            `json:"index"`
	Name  string         `json:"name"`
	File  string         `json:"file"`
	Data  map[string]any `json:"data"`
}

// ValidationReport is the full structured result returned by ValidateAppConfigFile,
// containing overall validity, severity-grouped issue lists, summary counts, and
// cleaned parsed application and profile maps.
type ValidationReport struct {
	Valid           bool                 `json:"valid"`
	Errors          []ValidationIssue    `json:"errors"`
	Warnings        []ValidationIssue    `json:"warnings"`
	Infos           []ValidationIssue    `json:"infos"`
	Issues          []ValidationIssue    `json:"issues"`
	Summary         ValidationSummary    `json:"summary"`
	ParsedAppConfig map[string]any       `json:"parsedAppConfig"`
	ParsedProfiles  []ParsedProfileEntry `json:"parsedProfiles"`
}

// AutoFixResult holds the repaired copy of a profile map returned by
// AutoFixProfileData along with a list of human-readable descriptions of fixes applied.
type AutoFixResult struct {
	FixedProfileData map[string]any `json:"fixedProfileData"`
	FixesApplied     []string       `json:"fixesApplied"`
}

// ValidateOptions configures contextual parameters for ValidateProfileData,
// including BaseDir for asset path checks, FileName for issue reporting, inherited
// AppParameters and AppLayout, and profile indexing bounds.
type ValidateOptions struct {
	BaseDir       string
	FileName      string
	AppParameters map[string]any
	AppLayout     *LayoutConfig
	ProfileCount  int
	ProfileIndex  int
}

// ValidateAppConfigFile parses and validates an application config file at
// configFilePath (resolved relative to baseDir when non-absolute, defaulting to
// the current working directory) and every referenced profile file. It validates
// top-level metadata, device/layout settings, parameters, and profile entries,
// returning a ValidationReport summarizing all errors, warnings, and infos.
func ValidateAppConfigFile(configFilePath string, baseDir string) ValidationReport {
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}
	resolvedConfigPath := configFilePath
	if !filepath.IsAbs(configFilePath) {
		resolvedConfigPath = filepath.Join(baseDir, configFilePath)
	}

	var issues []ValidationIssue
	addIssue := func(severity, file, path, message, suggestion string) {
		issues = append(issues, ValidationIssue{
			Severity:   severity,
			File:       file,
			Path:       path,
			Message:    message,
			Suggestion: suggestion,
		})
	}

	appConfig, parseErr := ParseConfigFileSafe(resolvedConfigPath)
	if parseErr != nil || appConfig == nil {
		msg := "Configuration file is empty or not an object."
		if parseErr != nil {
			msg = parseErr.Error()
		}
		addIssue("error", configFilePath, "", msg, "")
		return formatValidationResult(issues, nil, nil)
	}

	appName, ok := appConfig["application"].(string)
	if !ok || strings.TrimSpace(appName) == "" {
		addIssue(
			"error",
			configFilePath,
			"application",
			`Missing or invalid "application" attribute (must be a non-empty string).`,
			"Add `application: MyApplicationName` at the top of your config file.",
		)
	}

	validateDeviceAndLayoutFields(appConfig, configFilePath, addIssue)
	appLayout := NormalizeLayoutConfig(appConfig)

	if rawParams, exists := appConfig["parameters"]; exists && rawParams != nil {
		if _, ok := rawParams.(map[string]any); !ok {
			addIssue("error", configFilePath, "parameters", `"parameters" must be an object/dictionary if provided.`, "")
		}
	}

	rawProfiles, ok := appConfig["profiles"].([]any)
	if !ok || len(rawProfiles) == 0 {
		addIssue(
			"error",
			configFilePath,
			"profiles",
			`Missing or empty "profiles" list. At least one profile must be defined.`,
			`Add a `+"`profiles:`"+` array with `+"`{ name: \"profile-1\", file: \"config/profile-1.yaml\" }`"+`.`,
		)
		return formatValidationResult(issues, CleanInternalKeys(appConfig), nil)
	}

	if len(rawProfiles) > 7 {
		addIssue(
			"warning",
			configFilePath,
			"profiles",
			fmt.Sprintf("Config defines %d profiles, but default physical profile-switching buttons only cover up to 7 profiles (buttons 1..7).", len(rawProfiles)),
			"",
		)
	}

	var parsedProfiles []ParsedProfileEntry
	seenNames := make(map[string]bool)
	appParams, _ := appConfig["parameters"].(map[string]any)

	for i, item := range rawProfiles {
		entryPath := fmt.Sprintf("profiles[%d]", i)
		entry, ok := item.(map[string]any)
		if !ok || entry == nil {
			addIssue("error", configFilePath, entryPath, `Profile entry must be an object with "name" and "file".`, "")
			continue
		}
		nameStr, nameOk := entry["name"].(string)
		if !nameOk || strings.TrimSpace(nameStr) == "" {
			addIssue("error", configFilePath, entryPath+".name", `Profile entry is missing a valid "name" string.`, "")
		} else if seenNames[nameStr] {
			addIssue("warning", configFilePath, entryPath+".name", fmt.Sprintf(`Duplicate profile name "%s" in application config.`, nameStr), "")
		} else {
			seenNames[nameStr] = true
		}

		fileStr, fileOk := entry["file"].(string)
		if !fileOk || strings.TrimSpace(fileStr) == "" {
			addIssue("error", configFilePath, entryPath+".file", `Profile entry is missing a valid "file" path.`, "")
			continue
		}

		resolvedProfilePath := fileStr
		if !filepath.IsAbs(fileStr) {
			resolvedProfilePath = filepath.Join(baseDir, fileStr)
		}
		profileData, profErr := ParseConfigFileSafe(resolvedProfilePath)
		if profErr != nil || profileData == nil {
			msg := fmt.Sprintf("Cannot load profile file %s", fileStr)
			if profErr != nil {
				msg = profErr.Error()
			}
			addIssue("error", fileStr, "", msg, "")
			continue
		}

		profIssues := ValidateProfileData(profileData, ValidateOptions{
			FileName:      fileStr,
			BaseDir:       baseDir,
			AppParameters: appParams,
			AppLayout:     &appLayout,
			ProfileCount:  len(rawProfiles),
			ProfileIndex:  i,
		})
		issues = append(issues, profIssues...)
		parsedProfiles = append(parsedProfiles, ParsedProfileEntry{
			Index: i,
			Name:  nameStr,
			File:  fileStr,
			Data:  CleanInternalKeys(profileData),
		})
	}

	return formatValidationResult(issues, CleanInternalKeys(appConfig), parsedProfiles)
}

// ValidateProfileData validates a parsed profile map against opts and returns
// all discovered ValidationIssue items. It checks required "profile", "parameters",
// and "touch" fields, device and grid layout overrides, parameter formatting
// (including OPC UA endpointurl templates), and all control definitions under
// touch (center/left/right), knobs, and physical buttons.
func ValidateProfileData(profileData map[string]any, opts ValidateOptions) []ValidationIssue {
	fileName := opts.FileName
	if fileName == "" {
		fileName = "profile.yaml"
	}
	baseDir := opts.BaseDir
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}
	profileCount := opts.ProfileCount
	if profileCount <= 0 {
		profileCount = 1
	}

	var issues []ValidationIssue
	addIssue := func(severity, path, message, suggestion string) {
		issues = append(issues, ValidationIssue{
			Severity:   severity,
			File:       fileName,
			Path:       path,
			Message:    message,
			Suggestion: suggestion,
		})
	}

	if profileData == nil {
		addIssue("error", "", "Profile configuration must be a valid object.", "")
		return issues
	}

	if ToString(profileData["profile"]) == "" {
		addIssue(
			"error",
			"profile",
			`Missing required "profile" attribute (Profile.loadFromFile rejects files without it).`,
			"Add `profile: example` (or a descriptive identifier).",
		)
	}

	validateDeviceAndLayoutFields(profileData, fileName, func(sev, _f, p, msg, sug string) {
		addIssue(sev, p, msg, sug)
	})

	baseLayout := NormalizeLayoutConfig(nil)
	if opts.AppLayout != nil {
		baseLayout = *opts.AppLayout
	}
	effectiveLayout := baseLayout
	_, hasDev := profileData["device"]
	_, hasPrim := profileData["primaryDevice"]
	_, hasLay := profileData["layout"]
	_, hasCols := profileData["columns"]
	_, hasRows := profileData["rows"]
	if hasDev || hasPrim || hasLay || hasCols || hasRows {
		rawLay, _ := profileData["layout"].(map[string]any)
		merged := map[string]any{
			"device":  firstNonNil(profileData["device"], profileData["primaryDevice"], baseLayout.Device),
			"columns": firstNonNil(mapVal(rawLay, "columns"), profileData["columns"], baseLayout.Columns),
			"rows":    firstNonNil(mapVal(rawLay, "rows"), profileData["rows"], baseLayout.Rows),
			"layout":  profileData["layout"],
		}
		effectiveLayout = NormalizeLayoutConfig(merged)
	}

	profParams, hasProfParams := profileData["parameters"].(map[string]any)
	if !hasProfParams || profParams == nil {
		addIssue(
			"error",
			"parameters",
			`Missing required "parameters" object (Profile.loadFromFile rejects files without it).`,
			"Add a `parameters:` section (e.g. `hostname: 127.0.0.1`).",
		)
	}

	effectiveProfileParams := make(map[string]any)
	for k, v := range opts.AppParameters {
		if k != "__keys__" {
			effectiveProfileParams[k] = v
		}
	}
	for k, v := range profParams {
		if k != "__keys__" {
			effectiveProfileParams[k] = v
		}
	}

	if profParams != nil {
		if _, exists := profParams["textBaseLine"]; exists {
			addIssue(
				"warning",
				"parameters.textBaseLine",
				`Found "textBaseLine" (capital L), but Button rendering uses "textBaseline" (lowercase l).`,
				`Rename "textBaseLine" to "textBaseline".`,
			)
		}
		if ta, exists := profParams["textAlign"]; exists && !sliceContains(validTextAligns, ToString(ta)) {
			addIssue(
				"warning",
				"parameters.textAlign",
				fmt.Sprintf(`Invalid textAlign "%s". Expected one of: %s.`, ToString(ta), strings.Join(validTextAligns, ", ")),
				"",
			)
		}
		if tb, exists := profParams["textBaseline"]; exists && !sliceContains(validTextBaselines, ToString(tb)) {
			addIssue(
				"warning",
				"parameters.textBaseline",
				fmt.Sprintf(`Invalid textBaseline "%s". Expected one of: %s.`, ToString(tb), strings.Join(validTextBaselines, ", ")),
				"",
			)
		}
		if mpm, exists := profParams["minPressedMode"]; exists && !sliceContains(validMinPressedModes, ToString(mpm)) {
			addIssue(
				"warning",
				"parameters.minPressedMode",
				fmt.Sprintf(`Invalid minPressedMode "%s". Expected one of: %s.`, ToString(mpm), strings.Join(validMinPressedModes, ", ")),
				"",
			)
		}
		if ep, exists := profParams["endpointurl"]; exists {
			formattedUrl := FormatTemplate(ToString(ep), effectiveProfileParams)
			if !strings.HasPrefix(formattedUrl, "opc.tcp://") {
				addIssue(
					"warning",
					"parameters.endpointurl",
					fmt.Sprintf(`OPC UA endpointurl "%s" does not start with "opc.tcp://".`, formattedUrl),
					"Use format `opc.tcp://{hostname}:4840`.",
				)
			}
		}
	}

	defaultState, ok := profileData["default"].(map[string]any)
	if !ok || defaultState == nil {
		defaultState = map[string]any{"color": "#111111"}
	}

	cols := effectiveLayout.Columns
	if cols <= 0 {
		cols = 4
	}
	rows := effectiveLayout.Rows
	if rows <= 0 {
		rows = 3
	}
	maxCenterKeys := cols * rows

	touchMap, hasTouch := profileData["touch"].(map[string]any)
	if !hasTouch || touchMap == nil {
		addIssue("error", "touch", `Missing required "touch" object (Profile.loadFromFile rejects files without it).`, "")
	} else {
		targetSpec := ResolveDeviceSpec(firstNonNil(effectiveLayout.DeviceID, effectiveLayout.Device))
		if targetSpec == nil {
			targetSpec = DeviceSpecs["loupedeck-live"]
		}
		var missingTouch []string
		for _, d := range targetSpec.Displays {
			if d == "center" || d == "left" || d == "right" {
				if _, ok := touchMap[d].(map[string]any); !ok {
					missingTouch = append(missingTouch, fmt.Sprintf(`"touch.%s"`, d))
				}
			}
		}
		if len(missingTouch) > 0 {
			addIssue(
				"warning",
				"touch",
				fmt.Sprintf("Missing touch sub-section(s): %s. Built-in defaults will be used for omitted touch areas.", strings.Join(missingTouch, ", ")),
				"Ensure `touch.center`, `touch.left`, and `touch.right` are defined if you want custom behavior.",
			)
		}

		if centerMap, ok := touchMap["center"].(map[string]any); ok && centerMap != nil {
			for _, key := range OrderedKeys(centerMap) {
				keyIdx, isInt := ToInt(key)
				if !isInt || keyIdx < 0 || keyIdx >= maxCenterKeys {
					addIssue(
						"warning",
						fmt.Sprintf("touch.center.%s", key),
						fmt.Sprintf(`Touch center key "%s" is outside the configured %dx%d (%d-button) grid ("0" through "%d").`, key, cols, rows, maxCenterKeys, maxCenterKeys-1),
						"",
					)
				}
				validateButtonEntry(
					centerMap[key],
					key,
					fmt.Sprintf("touch.center.%s", key),
					effectiveProfileParams,
					defaultState,
					true,
					profileCount,
					baseDir,
					addIssue,
					false,
				)
			}
		}

		for _, side := range []string{"left", "right"} {
			if sideMap, ok := touchMap[side].(map[string]any); ok && sideMap != nil {
				for _, key := range OrderedKeys(sideMap) {
					if !sliceContains(validSideIDs, key) {
						addIssue(
							"warning",
							fmt.Sprintf("touch.%s.%s", side, key),
							fmt.Sprintf(`Touch %s strip only uses key "0", found "%s".`, side, key),
							"",
						)
					}
					validateButtonEntry(
						sideMap[key],
						key,
						fmt.Sprintf("touch.%s.%s", side, key),
						effectiveProfileParams,
						map[string]any{},
						false,
						profileCount,
						baseDir,
						addIssue,
						false,
					)
				}
			}
		}
	}

	if rawKnobs, exists := profileData["knobs"]; exists && rawKnobs != nil {
		knobsMap, ok := rawKnobs.(map[string]any)
		if !ok {
			addIssue("error", "knobs", `"knobs" must be an object mapping knob IDs (knobTL, knobCL, etc.) to configurations.`, "")
		} else {
			for _, knobKey := range OrderedKeys(knobsMap) {
				if !sliceContains(validKnobIDs, knobKey) {
					addIssue(
						"warning",
						fmt.Sprintf("knobs.%s", knobKey),
						fmt.Sprintf(`Unknown knob ID "%s". Valid knob IDs are: %s.`, knobKey, strings.Join(validKnobIDs, ", ")),
						"",
					)
				}
				validateButtonEntry(
					knobsMap[knobKey],
					knobKey,
					fmt.Sprintf("knobs.%s", knobKey),
					effectiveProfileParams,
					map[string]any{},
					false,
					profileCount,
					baseDir,
					addIssue,
					false,
				)
			}
		}
	}

	if rawButtons, exists := profileData["buttons"]; !exists || rawButtons == nil {
		addIssue(
			"info",
			"buttons",
			`No "buttons" section defined — physical buttons 0..7 will automatically be configured for brightness (0) and profile switching (1..N).`,
			"",
		)
	} else if buttonsMap, ok := rawButtons.(map[string]any); !ok {
		addIssue("error", "buttons", `"buttons" must be an object mapping physical button IDs ("0".."7") to configurations.`, "")
	} else {
		for _, btnKey := range OrderedKeys(buttonsMap) {
			if !sliceContains(validButtonIDs, btnKey) {
				addIssue(
					"warning",
					fmt.Sprintf("buttons.%s", btnKey),
					fmt.Sprintf(`Unknown physical button ID "%s". Loupedeck Live/CT physical buttons use IDs "0" through "7".`, btnKey),
					"",
				)
			}
			validateButtonEntry(
				buttonsMap[btnKey],
				btnKey,
				fmt.Sprintf("buttons.%s", btnKey),
				effectiveProfileParams,
				map[string]any{},
				false,
				profileCount,
				baseDir,
				addIssue,
				true,
			)
		}
	}

	return issues
}

// validateButtonEntry validates a single touch tile, physical button, or rotary knob
// configuration map at pathPrefix. It checks deprecated legacy root properties,
// params attributes (textBaseLine typo, type, filter, minPressed, minPressedMode,
// hex colors, image existence under baseDir, default state existence, nodeid, and
// blink), non-empty states, per-state colors/images/profile indices/brightness,
// undefined template placeholders in cmd/http/opcua/text, and HTTP or OPC UA
// endpoint and target validity, emitting issues through addIssue.
func validateButtonEntry(
	rawElem any,
	key string,
	pathPrefix string,
	effectiveProfileParams map[string]any,
	defaultState map[string]any,
	syncWithDefaultState bool,
	profileCount int,
	baseDir string,
	addIssue func(severity, path, message, suggestion string),
	isPhysicalButton bool,
) {
	elem, ok := rawElem.(map[string]any)
	if !ok || elem == nil {
		addIssue("error", pathPrefix, "Button configuration must be an object.", "")
		return
	}

	for _, legacyProp := range legacyButtonProps {
		if val, exists := elem[legacyProp]; exists {
			if legacyProp == "group" && ToString(val) == "" {
				addIssue(
					"warning",
					fmt.Sprintf("%s.%s", pathPrefix, legacyProp),
					fmt.Sprintf(`Legacy property "%s" at button root is deprecated.`, legacyProp),
					fmt.Sprintf(`Move "%s" inside "%s.params.%s" or remove if empty.`, legacyProp, pathPrefix, legacyProp),
				)
			} else {
				addIssue(
					"warning",
					fmt.Sprintf("%s.%s", pathPrefix, legacyProp),
					fmt.Sprintf(`Legacy property "%s" at button root is deprecated in Button constructor.`, legacyProp),
					fmt.Sprintf(`Move "%s" inside "%s.params.%s".`, legacyProp, pathPrefix, legacyProp),
				)
			}
		}
	}

	params, _ := elem["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}

	if _, exists := params["textBaseLine"]; exists {
		addIssue(
			"warning",
			pathPrefix+".params.textBaseLine",
			`Found "textBaseLine" (capital L), but Button uses "textBaseline" (lowercase l).`,
			`Rename to "textBaseline".`,
		)
	}

	if t, exists := params["type"]; exists && !sliceContains(validButtonTypes, ToString(t)) {
		addIssue(
			"warning",
			pathPrefix+".params.type",
			fmt.Sprintf(`Invalid button type "%s". Valid types are: "TOGGLE", "PUSH", or "".`, ToString(t)),
			"",
		)
	}

	if f, exists := params["filter"]; exists && !sliceContains(validFilters, ToString(f)) {
		addIssue(
			"warning",
			pathPrefix+".params.filter",
			fmt.Sprintf(`Invalid event filter "%s". Valid filters are: "pressed", "released", "rotated", or "".`, ToString(f)),
			"",
		)
	}

	if mp, exists := params["minPressed"]; exists {
		if mpVal, ok := ToFloat(mp); !ok || mpVal < 0 {
			addIssue(
				"warning",
				pathPrefix+".params.minPressed",
				fmt.Sprintf(`Invalid minPressed value "%v". Must be a non-negative number in milliseconds.`, mp),
				"",
			)
		}
	}

	if mpm, exists := params["minPressedMode"]; exists && !sliceContains(validMinPressedModes, ToString(mpm)) {
		addIssue(
			"warning",
			pathPrefix+".params.minPressedMode",
			fmt.Sprintf(`Invalid minPressedMode "%s". Valid modes are: %s.`, ToString(mpm), strings.Join(validMinPressedModes, ", ")),
			`Use "immediate" (switch immediately, vibrate after minPressed ms) or "delayed" (require holding for minPressed ms before switching).`,
		)
	}

	if col, exists := params["color"]; exists && !hexColorRegex.MatchString(ToString(col)) {
		addIssue(
			"warning",
			pathPrefix+".params.color",
			fmt.Sprintf(`Color "%s" is not a 6-digit hex color (#RRGGBB). darkerColor/colorToRGB requires #RRGGBB format.`, ToString(col)),
			`Use 6-digit hex format like "#00ff00".`,
		)
	}

	if tc, exists := params["textColor"]; exists && !hexColorRegex.MatchString(ToString(tc)) {
		addIssue(
			"warning",
			pathPrefix+".params.textColor",
			fmt.Sprintf(`textColor "%s" is not a 6-digit hex color (#RRGGBB).`, ToString(tc)),
			`Use 6-digit hex format like "#ffffff".`,
		)
	}

	if img, exists := params["image"]; exists && ToString(img) != "" {
		imgPath := filepath.Join(baseDir, ToString(img))
		if _, err := os.Stat(imgPath); err != nil {
			addIssue(
				"error",
				pathPrefix+".params.image",
				fmt.Sprintf(`Image file not found: "%s" (resolved to %s). When missing, Button.load() fails and stops loading remaining states!`, ToString(img), imgPath),
				"Verify the relative path to the icon file.",
			)
		}
	}

	statesMap, hasStates := elem["states"].(map[string]any)
	stateKeys := OrderedKeys(statesMap)
	if !hasStates || len(stateKeys) == 0 {
		addIssue(
			"error",
			pathPrefix+".states",
			`Button is missing a non-empty "states" dictionary.`,
			"",
		)
		return
	}

	configuredDefault := params["default"]
	if configuredDefault == nil {
		configuredDefault = elem["default"]
	}
	if configuredDefault != nil && ToString(configuredDefault) != "0" {
		if !sliceContains(stateKeys, ToString(configuredDefault)) {
			addIssue(
				"warning",
				pathPrefix+".params.default",
				fmt.Sprintf(`Configured default state "%s" does not exist in states [%s].`, ToString(configuredDefault), strings.Join(stateKeys, ", ")),
				fmt.Sprintf("Set default to one of: %s.", strings.Join(stateKeys, ", ")),
			)
		}
	}

	contextOptions := make(map[string]any)
	for k, v := range effectiveProfileParams {
		contextOptions[k] = v
	}
	for k, v := range params {
		if k != "__keys__" {
			contextOptions[k] = v
		}
	}
	contextOptions["key"] = key

	if nid, exists := params["nodeid"]; exists && ToString(nid) != "" {
		formattedNodeId := FormatTemplate(ToString(nid), contextOptions)
		if !nodeIDRegex.MatchString(formattedNodeId) {
			addIssue(
				"warning",
				pathPrefix+".params.nodeid",
				fmt.Sprintf(`OPC UA nodeid "%s" does not match standard format "ns=<index>;<s|i|b|g>=<id>".`, formattedNodeId),
				"",
			)
		}
	}

	if blinkStr, ok := params["blink"].(string); ok && blinkStr != "" {
		formattedBlink := FormatTemplate(blinkStr, contextOptions)
		if !nodeIDRegex.MatchString(formattedBlink) {
			addIssue(
				"warning",
				pathPrefix+".params.blink",
				fmt.Sprintf(`Blink attribute "%s" is a string but not a valid OPC UA NodeID ("ns=<index>;<s|i|b|g>=<id>"). Use boolean true/false or a valid NodeID.`, formattedBlink),
				"",
			)
		}
	}

	for _, stateName := range stateKeys {
		statePath := fmt.Sprintf("%s.states.%s", pathPrefix, stateName)
		rawState, ok := statesMap[stateName].(map[string]any)
		if !ok || rawState == nil {
			addIssue("error", statePath, fmt.Sprintf(`State "%s" must be an object.`, stateName), "")
			continue
		}

		state := CloneMap(rawState)
		if syncWithDefaultState {
			state = SyncParams(state, defaultState)
		}

		mergedOptions := map[string]any{
			"color":     "#000000",
			"textColor": "#ffffff",
			"min":       0,
			"max":       100,
			"value":     50,
			"nodeid":    "",
		}
		for k, v := range effectiveProfileParams {
			mergedOptions[k] = v
		}
		for k, v := range params {
			if k != "__keys__" {
				mergedOptions[k] = v
			}
		}
		for k, v := range state {
			if k != "__keys__" {
				mergedOptions[k] = v
			}
		}
		prefix := "touch"
		if isPhysicalButton {
			prefix = "button"
		}
		mergedOptions["id"] = fmt.Sprintf("%s-%s", prefix, key)
		mergedOptions["key"] = key
		mergedOptions["state"] = stateName

		if sc, exists := state["color"]; exists && sc != nil && !hexColorRegex.MatchString(ToString(sc)) {
			addIssue(
				"warning",
				statePath+".color",
				fmt.Sprintf(`State color "%s" is not a 6-digit hex color (#RRGGBB).`, ToString(sc)),
				`Use 6-digit hex format like "#000099".`,
			)
		}

		if stc, exists := state["textColor"]; exists && stc != nil && !hexColorRegex.MatchString(ToString(stc)) {
			addIssue(
				"warning",
				statePath+".textColor",
				fmt.Sprintf(`State textColor "%s" is not a 6-digit hex color (#RRGGBB).`, ToString(stc)),
				"",
			)
		}

		if simg, exists := state["image"]; exists && ToString(simg) != "" {
			imgPath := filepath.Join(baseDir, ToString(simg))
			if _, err := os.Stat(imgPath); err != nil {
				addIssue(
					"error",
					statePath+".image",
					fmt.Sprintf(`Image file not found: "%s" (resolved to %s).`, ToString(simg), imgPath),
					"Ensure the image file exists or update the path.",
				)
			}
		}

		if sprof, exists := state["profile"]; exists && ToString(sprof) != "" {
			if targetProf, ok := ToInt(sprof); !ok || targetProf < 0 || targetProf >= profileCount {
				maxP := profileCount - 1
				if maxP < 0 {
					maxP = 0
				}
				addIssue(
					"warning",
					statePath+".profile",
					fmt.Sprintf("State switches to profile index %v, which is outside the configured profile range (0..%d).", sprof, maxP),
					"",
				)
			}
		}

		if sb, exists := state["brightness"]; exists && ToString(sb) != "" {
			if bVal, ok := ToFloat(sb); !ok || bVal < 0 || bVal > 1 {
				addIssue(
					"warning",
					statePath+".brightness",
					fmt.Sprintf(`Brightness value "%v" should be a number between 0.0 and 1.0.`, sb),
					"",
				)
			}
		}

		if sblink, ok := state["blink"].(string); ok && sblink != "" {
			formattedBlink := FormatTemplate(sblink, mergedOptions)
			if !nodeIDRegex.MatchString(formattedBlink) {
				addIssue(
					"warning",
					statePath+".blink",
					fmt.Sprintf(`State blink "%s" is a string but not a valid OPC UA NodeID.`, formattedBlink),
					"",
				)
			}
		}

		knownKeys := make(map[string]bool)
		for _, k := range builtinButtonParams {
			knownKeys[k] = true
		}
		for k := range mergedOptions {
			knownKeys[k] = true
		}

		for _, field := range []string{"cmd", "http", "opcua", "text"} {
			if fStr, ok := state[field].(string); ok {
				for _, ph := range ExtractPlaceholders(fStr) {
					if !knownKeys[ph] {
						addIssue(
							"warning",
							fmt.Sprintf("%s.%s", statePath, field),
							fmt.Sprintf(`Template placeholder "{%s}" is not defined in button params, state, or profile parameters.`, ph),
							"",
						)
					}
				}
			}
		}

		if httpRaw, exists := mergedOptions["http"]; exists && ToString(httpRaw) != "" {
			if ToString(mergedOptions["hostname"]) == "" {
				addIssue(
					"error",
					statePath+".http",
					`HTTP action requires "hostname" parameter in profile parameters or button params (HTTPif.Check returns -21 without it).`,
					"",
				)
			}
			formattedHttp := FormatTemplate(ToString(httpRaw), mergedOptions)
			if parsedU, err := url.Parse(formattedHttp); err != nil || parsedU.Scheme == "" || parsedU.Host == "" {
				addIssue(
					"error",
					statePath+".http",
					fmt.Sprintf(`Formatted HTTP URL "%s" is not a valid URL.`, formattedHttp),
					"",
				)
			}
		}

		if opcuaRaw, exists := mergedOptions["opcua"]; exists && ToString(opcuaRaw) != "" {
			if ToString(mergedOptions["endpointurl"]) == "" {
				addIssue(
					"error",
					statePath+".opcua",
					`OPC UA action requires "endpointurl" in profile parameters or button params (OPCUAIf.Check returns -11 without it).`,
					"",
				)
			}
			formattedNode := FormatTemplate(ToString(opcuaRaw), mergedOptions)
			if !nodeIDRegex.MatchString(formattedNode) {
				addIssue(
					"error",
					statePath+".opcua",
					fmt.Sprintf(`OPC UA target "%s" is not a valid OPC UA NodeID (expected format: ns=<index>;<s|i|b|g>=<identifier>).`, formattedNode),
					"",
				)
			}
		}
	}
}

// AutoFixProfileData creates a cleaned deep copy of profileData (stripping
// internal "__keys__" metadata) and repairs common structural issues: adding missing
// top-level "profile", "parameters", and "touch" sections, fixing "textBaseLine"
// typos to "textBaseline", and migrating deprecated legacy root button properties
// into "params" (or removing empty "group" fields). It returns an AutoFixResult
// containing the repaired map and descriptions of each fix applied.
func AutoFixProfileData(profileData map[string]any) AutoFixResult {
	clone := CleanInternalKeys(CloneMap(profileData))
	if clone == nil {
		clone = map[string]any{}
	}
	var fixesApplied []string

	if ToString(clone["profile"]) == "" {
		name := ToString(clone["name"])
		if name == "" {
			name = "example"
		}
		clone["profile"] = name
		fixesApplied = append(fixesApplied, `Added missing top-level "profile" attribute`)
	}

	params, ok := clone["parameters"].(map[string]any)
	if !ok || params == nil {
		clone["parameters"] = map[string]any{"hostname": "127.0.0.1"}
		fixesApplied = append(fixesApplied, `Added missing top-level "parameters" section`)
	} else if val, exists := params["textBaseLine"]; exists {
		params["textBaseline"] = val
		delete(params, "textBaseLine")
		fixesApplied = append(fixesApplied, "Renamed parameters.textBaseLine -> parameters.textBaseline")
	}

	touch, ok := clone["touch"].(map[string]any)
	if !ok || touch == nil {
		touch = map[string]any{}
		clone["touch"] = touch
	}
	for _, side := range []string{"left", "right"} {
		if _, ok := touch[side].(map[string]any); !ok {
			touch[side] = map[string]any{
				"0": map[string]any{
					"states": map[string]any{
						"on": map[string]any{"color": "#000000", "cmd": `echo "{id} {state}"`},
					},
				},
			}
			fixesApplied = append(fixesApplied, fmt.Sprintf("Added missing touch.%s section", side))
		}
	}
	if _, ok := touch["center"].(map[string]any); !ok {
		touch["center"] = map[string]any{}
		fixesApplied = append(fixesApplied, "Added missing touch.center section")
	}

	fixButtonMap := func(rawMap any, sectionLabel string) {
		mapObj, ok := rawMap.(map[string]any)
		if !ok || mapObj == nil {
			return
		}
		for _, key := range OrderedKeys(mapObj) {
			elem, ok := mapObj[key].(map[string]any)
			if !ok || elem == nil {
				continue
			}
			for _, legacyProp := range legacyButtonProps {
				if val, exists := elem[legacyProp]; exists {
					if legacyProp == "group" && ToString(val) == "" {
						delete(elem, "group")
						fixesApplied = append(fixesApplied, fmt.Sprintf(`Removed empty legacy "%s.%s.group"`, sectionLabel, key))
					} else {
						pMap, ok := elem["params"].(map[string]any)
						if !ok || pMap == nil {
							pMap = map[string]any{}
							elem["params"] = pMap
						}
						if _, hasInParams := pMap[legacyProp]; !hasInParams {
							pMap[legacyProp] = val
						}
						delete(elem, legacyProp)
						fixesApplied = append(fixesApplied, fmt.Sprintf(`Moved legacy "%s.%s.%s" into params.%s`, sectionLabel, key, legacyProp, legacyProp))
					}
				}
			}
			if pMap, ok := elem["params"].(map[string]any); ok && pMap != nil {
				if val, exists := pMap["textBaseLine"]; exists {
					pMap["textBaseline"] = val
					delete(pMap, "textBaseLine")
					fixesApplied = append(fixesApplied, fmt.Sprintf(`Renamed "%s.%s.params.textBaseLine" -> textBaseline`, sectionLabel, key))
				}
			}
		}
	}

	fixButtonMap(touch["center"], "touch.center")
	fixButtonMap(touch["left"], "touch.left")
	fixButtonMap(touch["right"], "touch.right")
	fixButtonMap(clone["knobs"], "knobs")
	fixButtonMap(clone["buttons"], "buttons")

	if fixesApplied == nil {
		fixesApplied = []string{}
	}
	return AutoFixResult{
		FixedProfileData: clone,
		FixesApplied:     fixesApplied,
	}
}

// validateDeviceAndLayoutFields checks "device", "primaryDevice", and "layout" entries in configObj
// and emits warnings via addIssue for unknown models, invalid types, or out-of-range grid dimensions (1..16).
func validateDeviceAndLayoutFields(configObj map[string]any, fileLabel string, addIssue func(severity, file, path, message, suggestion string)) {
	if configObj == nil {
		return
	}
	rawDevice := configObj["device"]
	devPath := "device"
	if rawDevice == nil {
		rawDevice = configObj["primaryDevice"]
		devPath = "primaryDevice"
	}
	if rawDevice != nil {
		switch devVal := rawDevice.(type) {
		case string:
			if ResolveDeviceSpec(devVal) == nil {
				addIssue(
					"warning",
					fileLabel,
					devPath,
					fmt.Sprintf(`Unknown primary device model "%s". Supported models: Loupedeck Live, Loupedeck CT, Loupedeck Live S, Razer Stream Controller, Razer Stream Controller X.`, devVal),
					`Use e.g. `+"`device: \"Loupedeck Live\"`"+` or `+"`device: \"Loupedeck CT\"`"+`.`,
				)
			}
		case map[string]any:
			modelStr := firstNonNil(devVal["type"], devVal["model"], devVal["name"], devVal["id"])
			if modelStr != nil && ResolveDeviceSpec(modelStr) == nil {
				addIssue(
					"warning",
					fileLabel,
					"device",
					fmt.Sprintf(`Unknown primary device model "%v". Supported models: Loupedeck Live, Loupedeck CT, Loupedeck Live S, Razer Stream Controller, Razer Stream Controller X.`, modelStr),
					"",
				)
			}
		default:
			addIssue(
				"warning",
				fileLabel,
				"device",
				`"device" must be a device model string (e.g. "Loupedeck Live") or an object with model/layout properties.`,
				"",
			)
		}
	}

	if rawLayout, exists := configObj["layout"]; exists && rawLayout != nil {
		layMap, ok := rawLayout.(map[string]any)
		if !ok {
			addIssue("warning", fileLabel, "layout", `"layout" must be an object with e.g. { columns: 4, rows: 3 }.`, "")
		} else {
			layoutDevice := firstNonNil(layMap["device"], layMap["model"])
			if layoutDevice != nil && ResolveDeviceSpec(layoutDevice) == nil {
				addIssue("warning", fileLabel, "layout.device", fmt.Sprintf(`Unknown layout device model "%v".`, layoutDevice), "")
			}
			if rawCols, hasC := layMap["columns"]; hasC && rawCols != nil {
				if cols, ok := ToInt(rawCols); !ok || cols < 1 || cols > 16 {
					addIssue("warning", fileLabel, "layout.columns", fmt.Sprintf(`Invalid layout.columns "%v". Must be a positive integer (e.g. 4 or 5).`, rawCols), "")
				}
			}
			if rawRows, hasR := layMap["rows"]; hasR && rawRows != nil {
				if rows, ok := ToInt(rawRows); !ok || rows < 1 || rows > 16 {
					addIssue("warning", fileLabel, "layout.rows", fmt.Sprintf(`Invalid layout.rows "%v". Must be a positive integer (e.g. 3).`, rawRows), "")
				}
			}
		}
	}
}

// formatValidationResult groups issues by severity ("error", "warning", "info"), builds ValidationSummary counts,
// and returns the resulting ValidationReport.
func formatValidationResult(issues []ValidationIssue, parsedAppConfig map[string]any, parsedProfiles []ParsedProfileEntry) ValidationReport {
	if issues == nil {
		issues = []ValidationIssue{}
	}
	errors := make([]ValidationIssue, 0)
	warnings := make([]ValidationIssue, 0)
	infos := make([]ValidationIssue, 0)
	for _, iss := range issues {
		switch iss.Severity {
		case "error":
			errors = append(errors, iss)
		case "warning":
			warnings = append(warnings, iss)
		case "info":
			infos = append(infos, iss)
		}
	}
	if parsedProfiles == nil {
		parsedProfiles = []ParsedProfileEntry{}
	}
	return ValidationReport{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Infos:    infos,
		Issues:   issues,
		Summary: ValidationSummary{
			ErrorCount:   len(errors),
			WarningCount: len(warnings),
			InfoCount:    len(infos),
			ProfileCount: len(parsedProfiles),
		},
		ParsedAppConfig: parsedAppConfig,
		ParsedProfiles:  parsedProfiles,
	}
}

// sliceContains reports whether item exists in slice.
func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
