package commander

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/device"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/interfaces"
)

const (
	ButtonTypeNone   = ""
	ButtonTypeToggle = "TOGGLE"
	ButtonTypePush   = "PUSH"

	MinPressedImmediate = "immediate"
	MinPressedDelayed   = "delayed"
)

type Button struct {
	Mu               sync.RWMutex
	ID               string
	Profile          *config.Profile
	Params           map[string]interface{}
	States           map[string]map[string]interface{}
	Keys             []string
	ImgBuffers       map[string]image.Image
	Index            int
	IndexBeforePress int
	HoldTimer        *time.Timer
	EnforcedIndex    int
	EnforcedBlink    bool
	Inversed         bool
	Event            string
	PressActive      bool
	TimeStampPressed int64
	TimeStampRelease int64
	TimeHold         int64
	Counter          int
	Ifaces           *interfaces.Manager
}

func NewButton(id string, width, height float64, data map[string]interface{}, key string, profile *config.Profile, offsetX, offsetY float64, ifaces *interfaces.Manager) *Button {
	b := &Button{
		ID:            id,
		Profile:       profile,
		States:        make(map[string]map[string]interface{}),
		Keys:          []string{},
		ImgBuffers:    make(map[string]image.Image),
		Index:         0,
		EnforcedIndex: -1,
		EnforcedBlink: false,
		Inversed:      false,
		Ifaces:        ifaces,
		Params: map[string]interface{}{
			"key":            key,
			"state":          "",
			"width":          width,
			"height":         height,
			"offsetX":        offsetX,
			"offsetY":        offsetY,
			"min":            0.0,
			"max":            100.0,
			"x":              0.0,
			"y":              0.0,
			"color":          "#000000",
			"text":           "",
			"textColor":      "#ffffff",
			"textAlign":      "center",
			"textBaseline":   "top",
			"font":           "16px Arial",
			"value":          50.0,
			"touchvalue":     50.0,
			"minPressed":     0.0,
			"minPressedMode": MinPressedImmediate,
			"moveLeft":       false,
			"moveRight":      false,
			"moveUp":         false,
			"moveDown":       false,
			"nodeid":         "",
			"type":           ButtonTypeToggle,
			"group":          key,
			"filter":         "",
			"blink":          false,
			"vibrate":        false,
			"profile":        "",
			"brightness":     nil,
			"default":        "0",
			"image":          "",
		},
	}

	if profile != nil && profile.Parameters != nil {
		for _, k := range config.OrderedKeys(profile.Parameters) {
			b.Params[k] = profile.Parameters[k]
		}
	}
	b.Params["key"] = key

	if data != nil {
		if rawParams, ok := data["params"].(map[string]interface{}); ok && rawParams != nil {
			for _, k := range config.OrderedKeys(rawParams) {
				b.Params[k] = rawParams[k]
			}
			merged := b.getParamsLocked(nil)
			b.Params["group"] = config.FormatTemplate(config.ToString(b.Params["group"]), merged)
			b.Params["nodeid"] = config.FormatTemplate(config.ToString(b.Params["nodeid"]), merged)
		}

		if rawStates, ok := data["states"].(map[string]interface{}); ok && rawStates != nil {
			keys := config.OrderedKeys(rawStates)
			b.Keys = keys
			for _, k := range keys {
				if sm, ok := rawStates[k].(map[string]interface{}); ok && sm != nil {
					b.States[k] = config.CloneMap(sm)
				} else {
					b.States[k] = make(map[string]interface{})
				}
			}
			defStr := config.ToString(b.Params["default"])
			defIdx := indexOf(b.Keys, defStr)
			if defIdx >= 0 {
				b.Index = defIdx
			} else if len(b.Keys) > 0 {
				b.Params["default"] = b.Keys[0]
			}
			if len(b.Keys) > 0 && b.Index >= 0 && b.Index < len(b.Keys) {
				b.Params["state"] = b.Keys[b.Index]
			}
		}

		// Legacy properties
		if v, ok := data["minPressed"]; ok && v != nil {
			b.Params["minPressed"] = v
		}
		if v, ok := data["text"]; ok && v != nil {
			b.Params["text"] = v
		}
		if v, ok := data["nodeid"]; ok && v != nil {
			b.Params["nodeid"] = config.FormatTemplate(config.ToString(v), b.getParamsLocked(nil))
		}
		if v, ok := data["type"]; ok && v != nil {
			b.Params["type"] = v
		}
		if v, ok := data["default"]; ok && v != nil {
			if idx := indexOf(b.Keys, config.ToString(v)); idx >= 0 {
				b.Index = idx
			}
		}
		if v, ok := data["group"]; ok && v != nil {
			b.Params["group"] = config.FormatTemplate(config.ToString(v), b.getParamsLocked(nil))
		}
	}

	return b
}

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func (b *Button) SetState(val interface{}) {
	b.Mu.Lock()
	defer b.Mu.Unlock()

	switch v := val.(type) {
	case string:
		idx := indexOf(b.Keys, v)
		if idx >= 0 {
			b.Index = idx
		} else if v == "on" && len(b.Keys) > 1 {
			b.Index = 1
		} else if v == "off" {
			b.Index = 0
		} else if n, err := strconv.Atoi(v); err == nil {
			b.Index = n
		}
	case int:
		b.Index = v
	case float64:
		b.Index = int(v)
	}

	if len(b.Keys) > 0 {
		if b.Index < 0 || b.Index >= len(b.Keys) {
			b.Index = ((b.Index % len(b.Keys)) + len(b.Keys)) % len(b.Keys)
		}
		b.Params["state"] = b.Keys[b.Index]
	}
}

func parseTargetProfileInt(val interface{}) (int, bool) {
	if val == nil {
		return 0, false
	}
	s := strings.TrimSpace(config.ToString(val))
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f != math.Trunc(f) || f < 0 {
		return 0, false
	}
	return int(f), true
}

func (b *Button) GetTargetProfile() (int, bool) {
	b.Mu.RLock()
	defer b.Mu.RUnlock()

	if p, ok := parseTargetProfileInt(b.Params["profile"]); ok {
		return p, true
	}
	for _, k := range b.Keys {
		if elem := b.States[k]; elem != nil {
			if p, ok := parseTargetProfileInt(elem["profile"]); ok {
				return p, true
			}
		}
	}
	return 0, false
}

func (b *Button) getCurrentElementLocked() map[string]interface{} {
	if b.Index < 0 || b.Index >= len(b.Keys) {
		return nil
	}
	state := b.Keys[b.Index]
	return b.States[state]
}

func (b *Button) getParamsLocked(elem map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	if b.Profile != nil && b.Profile.Parameters != nil {
		for k, v := range b.Profile.Parameters {
			out[k] = v
		}
	}
	for k, v := range b.Params {
		out[k] = v
	}
	for k, v := range elem {
		out[k] = v
	}
	stateStr := config.ToString(b.Params["state"])
	if len(b.Keys) > 0 && b.Index >= 0 && b.Index < len(b.Keys) {
		stateStr = b.Keys[b.Index]
	}
	out["id"] = b.ID
	out["state"] = stateStr
	out["event"] = b.Event
	out["pressed"] = b.Event == "pressed"
	out["released"] = b.Event == "released"
	out["rotated"] = b.Event == "rotated"
	return out
}

func (b *Button) GetSnapshot() map[string]interface{} {
	b.Mu.RLock()
	defer b.Mu.RUnlock()

	currentStateName := ""
	if b.Index >= 0 && b.Index < len(b.Keys) {
		currentStateName = b.Keys[b.Index]
	}
	currentElem := b.getCurrentElementLocked()
	merged := b.getParamsLocked(currentElem)

	rawText := config.ToString(merged["text"])
	formattedText := ""
	if rawText != "" {
		formattedText = config.FormatTemplate(rawText, merged)
	}
	isBlink := isTruthyBlink(merged["blink"]) || b.EnforcedBlink
	availCopy := make([]string, len(b.Keys))
	copy(availCopy, b.Keys)

	return map[string]interface{}{
		"id":              b.ID,
		"key":             b.Params["key"],
		"currentIndex":    b.Index,
		"currentState":    currentStateName,
		"availableStates": availCopy,
		"pressActive":     b.PressActive,
		"inversed":        b.Inversed,
		"enforcedIndex":   b.EnforcedIndex,
		"enforcedBlink":   b.EnforcedBlink,
		"group":           b.Params["group"],
		"value":           b.Params["value"],
		"nodeid":          b.Params["nodeid"],
		"color":           merged["color"],
		"textColor":       merged["textColor"],
		"text":            formattedText,
		"rawText":         rawText,
		"image":           config.ToString(merged["image"]),
		"blink":           isBlink,
		"type":            b.Params["type"],
		"minPressed":      b.Params["minPressed"],
		"minPressedMode":  b.Params["minPressedMode"],
	}
}

func isTruthyBlink(val interface{}) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "1" || lower == "on"
	default:
		return false
	}
}

func (b *Button) IsBlinking() bool {
	b.Mu.RLock()
	defer b.Mu.RUnlock()
	elem := b.getCurrentElementLocked()
	params := b.getParamsLocked(elem)
	if isTruthyBlink(params["blink"]) {
		return true
	}
	return b.EnforcedBlink
}

func (b *Button) GetGroup() string {
	b.Mu.RLock()
	defer b.Mu.RUnlock()
	return config.ToString(b.Params["group"])
}

func (b *Button) DrawPhysical(dev device.Device, id string) error {
	b.Mu.RLock()
	elem := b.getCurrentElementLocked()
	if elem == nil {
		b.Mu.RUnlock()
		return nil
	}
	params := b.getParamsLocked(elem)
	colorStr := config.ToString(elem["color"])
	if colorStr == "" {
		colorStr = config.ToString(params["color"])
	}
	b.Mu.RUnlock()

	if colorStr == "" || len(colorStr) < 7 {
		return nil
	}
	r, err1 := strconv.ParseInt(colorStr[1:3], 16, 32)
	g, err2 := strconv.ParseInt(colorStr[3:5], 16, 32)
	bl, err3 := strconv.ParseInt(colorStr[5:7], 16, 32)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil
	}

	var targetID interface{} = id
	if idx, err := strconv.Atoi(id); err == nil {
		targetID = idx
	}
	rgbaStr := fmt.Sprintf("rgba(%d, %d, %d)", r, g, bl)
	return dev.SetButtonColor(targetID, rgbaStr)
}

func (b *Button) Draw(row, column int, ctx *device.Context2D) {
	b.Mu.Lock()
	elem := b.getCurrentElementLocked()
	params := b.getParamsLocked(elem)
	stateKey := ""
	if b.Index >= 0 && b.Index < len(b.Keys) {
		stateKey = b.Keys[b.Index]
	}
	imgBuf := b.ImgBuffers[stateKey]

	x := config.ToFloatDefault(params["offsetX"], 0)
	y := config.ToFloatDefault(params["offsetY"], 0)
	w := config.ToFloatDefault(params["width"], 90)
	h := config.ToFloatDefault(params["height"], 90)

	if w > h {
		x += float64(2+column) * (w - h) / 2.0
		w = h
	}

	b.Counter++
	if ctx == nil {
		b.Mu.Unlock()
		return
	}

	x += float64(column) * w
	y += float64(row) * h

	if isTruthyBlink(params["blink"]) || b.EnforcedBlink {
		b.Inversed = !b.Inversed
	}

	colorStr := config.ToString(params["color"])
	textColorStr := config.ToString(params["textColor"])
	if b.Inversed {
		colorStr = config.ToString(params["textColor"])
		textColorStr = config.ToString(params["color"])
	}
	if b.PressActive {
		colorStr = config.DarkerColor(colorStr, 0.5)
	}
	b.Mu.Unlock()

	ctx.FillStyle = colorStr
	ctx.FillRect(x, y, w, h)

	if imgBuf != nil {
		ctx.DrawImage(imgBuf, x, y, w, h)
	}

	rawText := config.ToString(params["text"])
	if rawText != "" {
		ctx.FillStyle = textColorStr
		ctx.Font = config.ToString(params["font"])
		textAlign := config.ToString(params["textAlign"])
		textBaseline := config.ToString(params["textBaseline"])
		ctx.TextAlign = textAlign
		ctx.TextBaseline = textBaseline

		dynamicText := config.FormatTemplate(rawText, params)
		tx := x
		ty := y
		switch textAlign {
		case "center":
			tx += w / 2.0
		case "left":
			tx += 12.0
		case "right":
			tx += w - 12.0
		}
		switch textBaseline {
		case "middle":
			ty += h / 2.0
		case "top":
			ty += 6.0
			ctx.TextBaseline = "top"
		case "bottom":
			ty += h - 12.0
			ctx.TextBaseline = "bottom"
		}
		ctx.FillText(dynamicText, tx, ty)
	}
}

func (b *Button) Load(globalConfig *config.Profile) bool {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	b.Profile = globalConfig
	allLoaded := true
	for _, key := range b.Keys {
		elem := b.States[key]
		if elem == nil {
			continue
		}
		file := config.ToString(elem["image"])
		if file == "" {
			file = config.ToString(b.Params["image"])
		}
		if file != "" {
			resolvedFile := file
			if globalConfig != nil && globalConfig.BaseDir != "" && !strings.HasPrefix(file, "/") {
				resolvedFile = globalConfig.BaseDir + "/" + file
			}
			img, err := device.LoadCachedImage(resolvedFile)
			if err != nil {
				img, err = device.LoadCachedImage(file)
			}
			if err != nil {
				allLoaded = false
			} else {
				b.ImgBuffers[key] = img
			}
		}
	}
	return allLoaded
}

func (b *Button) Pressed() bool {
	b.Mu.Lock()
	if b.HoldTimer != nil {
		b.HoldTimer.Stop()
		b.HoldTimer = nil
	}

	b.PressActive = true
	b.TimeStampPressed = time.Now().UnixMilli()
	b.IndexBeforePress = b.Index

	currentElem := b.getCurrentElementLocked()
	currentParams := b.getParamsLocked(currentElem)
	minPressed := config.ToIntDefault(currentParams["minPressed"], 0)
	mode := config.ToString(currentParams["minPressedMode"])
	if mode == "" {
		mode = MinPressedImmediate
	}

	var vibratePattern interface{} = byte(0x01)
	if v, exists := currentParams["vibrate"]; exists && v != nil && v != "" && v != false {
		vibratePattern = v
	}

	if minPressed > 0 {
		b.HoldTimer = time.AfterFunc(time.Duration(minPressed)*time.Millisecond, func() {
			b.Mu.Lock()
			b.HoldTimer = nil
			stillActive := b.PressActive
			if stillActive {
				b.TimeHold = time.Now().UnixMilli() - b.TimeStampPressed
			}
			ifaces := b.Ifaces
			b.Mu.Unlock()
			if stillActive && ifaces != nil {
				ifaces.EmitProfileEvent(interfaces.ProfileEvent{
					Type:  "vibrate",
					Value: vibratePattern,
				})
			}
		})
	}

	executeOnPress := (mode == MinPressedImmediate) || (minPressed <= 0)
	idxForExec := b.Index
	b.Index = idxForExec
	b.Event = "pressed"
	b.Index++

	if b.EnforcedIndex >= 0 && b.Index != b.EnforcedIndex {
		b.Index = b.EnforcedIndex
	}
	if len(b.Keys) > 0 {
		if b.Index < 0 {
			b.Index = len(b.Keys) - 1
		}
		b.Index %= len(b.Keys)
		b.Params["state"] = b.Keys[b.Index]
	}
	b.Mu.Unlock()

	if executeOnPress {
		// Temporarily run command using idxForExec just like updateState(this.#index, "pressed", executeOnPress) before this.#index++
		b.runCommandAtIndex(idxForExec, "pressed")
	}
	return true
}

func (b *Button) Released() bool {
	b.Mu.Lock()
	if b.HoldTimer != nil {
		b.HoldTimer.Stop()
		b.HoldTimer = nil
	}
	if !b.PressActive {
		b.Mu.Unlock()
		return false
	}

	b.PressActive = false
	b.Inversed = false
	elem := b.getCurrentElementLocked()
	if elem == nil {
		b.Mu.Unlock()
		return false
	}
	b.TimeStampRelease = time.Now().UnixMilli()
	b.TimeHold = b.TimeStampRelease - b.TimeStampPressed

	currentParams := b.getParamsLocked(elem)
	minPressed := config.ToIntDefault(currentParams["minPressed"], 0)
	mode := config.ToString(currentParams["minPressedMode"])
	if mode == "" {
		mode = MinPressedImmediate
	}

	if minPressed > 0 && int(b.TimeHold) < minPressed {
		if mode == MinPressedDelayed {
			b.Index = b.IndexBeforePress
			b.Event = "released"
			b.Mu.Unlock()
			return false
		}
	}

	btnType := config.ToString(b.Params["type"])
	if btnType != ButtonTypeToggle {
		b.Index--
	}

	if b.EnforcedIndex >= 0 && b.EnforcedIndex != b.Index {
		b.Index = b.EnforcedIndex
	}
	if len(b.Keys) > 0 {
		if b.Index < 0 {
			b.Index = len(b.Keys) - 1
		}
		b.Index %= len(b.Keys)
		b.Params["state"] = b.Keys[b.Index]
	}
	b.Event = "released"
	targetIdx := b.Index
	b.Mu.Unlock()

	b.runCommandAtIndex(targetIdx, "released")
	return true
}

func (b *Button) Rotated(delta int) bool {
	b.Mu.Lock()
	if b.getCurrentElementLocked() == nil {
		b.Mu.Unlock()
		return false
	}
	b.Event = "rotated"
	curVal := config.ToFloatDefault(b.Params["value"], 50)
	minVal := config.ToFloatDefault(b.Params["min"], 0)
	maxVal := config.ToFloatDefault(b.Params["max"], 100)
	b.Params["value"] = config.CalcDelta(curVal, float64(delta), minVal, maxVal)
	targetIdx := b.Index
	b.Mu.Unlock()

	b.runCommandAtIndex(targetIdx, "rotated")
	return true
}

func (b *Button) ButtonStateChanged(buttonID int, attribute, nodeid string, val interface{}) {
	if val == nil {
		return
	}
	b.Mu.Lock()
	defer b.Mu.Unlock()

	if attribute == "nodeid" && nodeid == config.ToString(b.Params["nodeid"]) {
		b.Index = 0
		b.EnforcedIndex = 0
		valStr := config.ToString(val)
		for i, k := range b.Keys {
			if valStr == k {
				b.EnforcedIndex = i
				break
			}
		}
		b.Index = b.EnforcedIndex
		if len(b.Keys) > 0 && b.Index >= 0 && b.Index < len(b.Keys) {
			b.Params["state"] = b.Keys[b.Index]
		}
		b.Inversed = false
		b.PressActive = false
	}

	if attribute == "blink" {
		b.EnforcedBlink = isTruthyBlink(val)
	}
}

func (b *Button) TouchMove(x, y float64) bool {
	b.Mu.Lock()
	defer b.Mu.Unlock()

	delta := 0.0
	curX := config.ToFloatDefault(b.Params["x"], 0)
	curY := config.ToFloatDefault(b.Params["y"], 0)
	if x > curX {
		b.Params["moveRight"] = true
		b.Params["moveLeft"] = false
	} else if x < curX {
		b.Params["moveRight"] = false
		b.Params["moveLeft"] = true
	}

	if y > curY {
		b.Params["moveDown"] = true
		b.Params["moveUp"] = false
		delta = -1
	} else if y < curY {
		b.Params["moveDown"] = false
		b.Params["moveUp"] = true
		delta = 1
	}

	b.Params["x"] = x
	b.Params["y"] = y

	baseTouchVal := config.ToFloatDefault(b.Params["touchvalue"], config.ToFloatDefault(b.Params["value"], 50))
	minVal := config.ToFloatDefault(b.Params["min"], 0)
	maxVal := config.ToFloatDefault(b.Params["max"], 100)
	b.Params["touchvalue"] = config.CalcDelta(baseTouchVal, delta, minVal, maxVal)
	return false
}

func (b *Button) runCommandAtIndex(idx int, eventType string) {
	b.Mu.RLock()
	var elem map[string]interface{}
	if idx >= 0 && idx < len(b.Keys) {
		elem = b.States[b.Keys[idx]]
	}
	params := b.getParamsLocked(elem)
	if idx >= 0 && idx < len(b.Keys) {
		params["state"] = b.Keys[idx]
	}
	params["event"] = eventType
	params["pressed"] = eventType == "pressed"
	params["released"] = eventType == "released"
	params["rotated"] = eventType == "rotated"
	ifaces := b.Ifaces
	b.Mu.RUnlock()

	if params == nil {
		return
	}
	filterStr := config.ToString(params["filter"])
	if filterStr != "" && filterStr != eventType {
		return
	}

	if ifaces != nil {
		if profVal, exists := params["profile"]; exists && profVal != nil && config.ToString(profVal) != "" {
			ifaces.EmitProfileEvent(interfaces.ProfileEvent{
				Type:  "profileChanged",
				Value: profVal,
			})
		}
		if brightVal, exists := params["brightness"]; exists && brightVal != nil && config.ToString(brightVal) != "" {
			ifaces.EmitProfileEvent(interfaces.ProfileEvent{
				Type:  "brightnessChanged",
				Value: brightVal,
			})
		}
		minPressed := config.ToIntDefault(params["minPressed"], 0)
		if vibVal, exists := params["vibrate"]; exists && vibVal != nil && vibVal != "" && vibVal != false && minPressed <= 0 {
			ifaces.EmitProfileEvent(interfaces.ProfileEvent{
				Type:  "vibrate",
				Value: vibVal,
			})
		}

		if cmdVal, hasCmd := params["cmd"]; hasCmd && ifaces.Shell != nil {
			_, _ = ifaces.Shell.Call(config.ToString(cmdVal), params)
		}
		if httpVal, hasHTTP := params["http"]; hasHTTP && ifaces.HTTP != nil {
			_, _ = ifaces.HTTP.Call(config.ToString(httpVal), params)
		}
		if opcuaVal, hasOPCUA := params["opcua"]; hasOPCUA && ifaces.OPCUA != nil {
			_, _ = ifaces.OPCUA.Call(config.ToString(opcuaVal), params)
		}
	}
}

