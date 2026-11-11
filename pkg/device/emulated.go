package device

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StateChangeEvent struct {
	Type         string                 `json:"type"`
	Brightness   float64                `json:"brightness,omitempty"`
	Pattern      byte                   `json:"pattern,omitempty"`
	Timestamp    int64                  `json:"timestamp,omitempty"`
	ID           interface{}            `json:"id,omitempty"`
	Color        string                 `json:"color,omitempty"`
	ButtonColors map[string]string      `json:"buttonColors,omitempty"`
	Screen       string                 `json:"screen,omitempty"`
	DataURL      string                 `json:"dataUrl,omitempty"`
}

type EmulatedDeviceOptions struct {
	Model   string
	Columns int
	Rows    int
}

type EmulatedLoupedeckDevice struct {
	Mu            sync.RWMutex
	Type          string
	Model         string
	Rows          int
	Columns       int
	KeySize       int
	VisibleX      [2]int
	Brightness    float64
	LastVibration *VibrationState
	Displays      map[string]DisplayInfo
	Knobs         []string
	Buttons       []interface{}
	ButtonColors  map[string]string
	ScreenDataURL map[string]string
	Canvases      map[string]*Canvas
	Connected     bool

	OnConnect     func(address string)
	OnDisconnect  func(err error)
	OnDown        func(id interface{})
	OnUp          func(id interface{})
	OnRotate      func(id string, delta int)
	OnTouchStart  func(data TouchEventData)
	OnTouchMove   func(data TouchEventData)
	OnTouchEnd    func(data TouchEventData)
	OnStateChange func(evt StateChangeEvent)
}

func NewEmulatedLoupedeckDevice(opts EmulatedDeviceOptions) *EmulatedLoupedeckDevice {
	d := &EmulatedLoupedeckDevice{
		Type:       "Loupedeck Live (Emulated)",
		Model:      "live",
		Rows:       3,
		Columns:    4,
		KeySize:    90,
		VisibleX:   [2]int{0, 480},
		Brightness: 1.0,
		Displays: map[string]DisplayInfo{
			"center": {Width: 360, Height: 270},
			"left":   {Width: 60, Height: 270},
			"right":  {Width: 60, Height: 270},
		},
		Knobs:         []string{"knobTL", "knobCL", "knobBL", "knobTR", "knobCR", "knobBR"},
		Buttons:       []interface{}{0, 1, 2, 3, 4, 5, 6, 7},
		ButtonColors:  make(map[string]string),
		ScreenDataURL: make(map[string]string),
		Canvases:      make(map[string]*Canvas),
	}

	rawModel := strings.ToLower(strings.TrimSpace(opts.Model))
	switch rawModel {
	case "ct", "loupedeck-ct", "loupedeck ct":
		d.Model = "ct"
		d.Type = "Loupedeck CT (Emulated)"
		d.Displays["knob"] = DisplayInfo{Width: 240, Height: 240}
	case "live-s", "lives", "loupedeck-live-s", "loupedeck live s":
		d.Model = "live-s"
		d.Type = "Loupedeck Live S (Emulated)"
		d.Rows = 3
		d.Columns = 5
		d.KeySize = 90
		d.VisibleX = [2]int{15, 465}
		d.Displays = map[string]DisplayInfo{
			"center": {Width: 480, Height: 270},
		}
		d.Knobs = []string{"knobCL", "knobTL"}
		d.Buttons = []interface{}{0, 1, 2, 3}
	case "razer-x", "razer-stream-controller-x", "razer stream controller x":
		d.Model = "razer-x"
		d.Type = "Razer Stream Controller X (Emulated)"
		d.Rows = 3
		d.Columns = 5
		d.KeySize = 96
		d.VisibleX = [2]int{0, 480}
		d.Displays = map[string]DisplayInfo{
			"center": {Width: 480, Height: 288},
		}
		d.Knobs = []string{}
		d.Buttons = []interface{}{}
	case "razer", "razer-stream-controller", "razer stream controller":
		d.Model = "razer"
		d.Type = "Razer Stream Controller (Emulated)"
	}

	if opts.Columns > 0 {
		d.Columns = opts.Columns
	}
	if opts.Rows > 0 {
		d.Rows = opts.Rows
	}

	d.initCanvases()
	return d
}

func (d *EmulatedLoupedeckDevice) initCanvases() {
	for name, dim := range d.Displays {
		c := NewCanvas(dim.Width, dim.Height)
		ctx := c.GetContext2D()
		ctx.FillStyle = "#000000"
		ctx.FillRect(0, 0, float64(dim.Width), float64(dim.Height))
		d.Canvases[name] = c
		d.ScreenDataURL[name] = c.ToDataURL()
	}
}

func (d *EmulatedLoupedeckDevice) GetType() string                   { return d.Type }
func (d *EmulatedLoupedeckDevice) GetModel() string                  { return d.Model }
func (d *EmulatedLoupedeckDevice) GetRows() int                      { return d.Rows }
func (d *EmulatedLoupedeckDevice) GetColumns() int                   { return d.Columns }
func (d *EmulatedLoupedeckDevice) GetKeySize() int                   { return d.KeySize }
func (d *EmulatedLoupedeckDevice) GetVisibleX() [2]int               { return d.VisibleX }
func (d *EmulatedLoupedeckDevice) GetDisplays() map[string]DisplayInfo { return d.Displays }
func (d *EmulatedLoupedeckDevice) GetButtons() []interface{}         { return d.Buttons }
func (d *EmulatedLoupedeckDevice) GetKnobs() []string                { return d.Knobs }

func (d *EmulatedLoupedeckDevice) GetScreenDataURL(screenName string) string {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.ScreenDataURL[screenName]
}

func (d *EmulatedLoupedeckDevice) Connect() error {
	d.Mu.Lock()
	d.Connected = true
	cb := d.OnConnect
	d.Mu.Unlock()
	if cb != nil {
		cb("emulator://localhost")
	}
	return nil
}

func (d *EmulatedLoupedeckDevice) Close() error {
	d.Mu.Lock()
	d.Connected = false
	cb := d.OnDisconnect
	d.Mu.Unlock()
	if cb != nil {
		cb(fmt.Errorf("Emulated device closed"))
	}
	return nil
}

func (d *EmulatedLoupedeckDevice) SetBrightness(value float64) error {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	d.Mu.Lock()
	d.Brightness = value
	cb := d.OnStateChange
	d.Mu.Unlock()
	if cb != nil {
		cb(StateChangeEvent{
			Type:       "brightness",
			Brightness: value,
		})
	}
	return nil
}

func (d *EmulatedLoupedeckDevice) Vibrate(pattern byte) error {
	ts := time.Now().UnixMilli()
	d.Mu.Lock()
	d.LastVibration = &VibrationState{
		Pattern:   pattern,
		Timestamp: ts,
	}
	cb := d.OnStateChange
	d.Mu.Unlock()
	if cb != nil {
		cb(StateChangeEvent{
			Type:      "vibrate",
			Pattern:   pattern,
			Timestamp: ts,
		})
	}
	return nil
}

func (d *EmulatedLoupedeckDevice) SetButtonColor(id interface{}, color string) error {
	key := fmt.Sprintf("%v", id)
	d.Mu.Lock()
	prev := d.ButtonColors[key]
	d.ButtonColors[key] = color
	colorsCopy := make(map[string]string, len(d.ButtonColors))
	for k, v := range d.ButtonColors {
		colorsCopy[k] = v
	}
	cb := d.OnStateChange
	d.Mu.Unlock()
	if prev != color && cb != nil {
		cb(StateChangeEvent{
			Type:         "buttonColor",
			ID:           id,
			Color:        color,
			ButtonColors: colorsCopy,
		})
	}
	return nil
}

func (d *EmulatedLoupedeckDevice) DrawScreen(screenName string, drawCallback func(ctx *Context2D) error) error {
	d.Mu.Lock()
	canvas, exists := d.Canvases[screenName]
	if !exists {
		d.Mu.Unlock()
		return nil
	}
	ctx := canvas.GetContext2D()
	ctx.Save()
	ctx.GlobalCompositeOperation = "source-over"
	ctx.FillStyle = "#000000"
	ctx.FillRect(0, 0, float64(canvas.Width), float64(canvas.Height))
	if drawCallback != nil {
		_ = drawCallback(ctx)
	}
	ctx.Restore()
	dataURL := canvas.ToDataURL()
	changed := d.ScreenDataURL[screenName] != dataURL
	if changed {
		d.ScreenDataURL[screenName] = dataURL
	}
	cb := d.OnStateChange
	d.Mu.Unlock()

	if changed && cb != nil {
		cb(StateChangeEvent{
			Type:    "screen",
			Screen:  screenName,
			DataURL: dataURL,
		})
	}
	return nil
}

func normalizeTriggerID(id interface{}) interface{} {
	switch v := id.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		s := strings.TrimSpace(v)
		if n, err := strconv.Atoi(s); err == nil && s != "" {
			return n
		}
		return s
	default:
		s := fmt.Sprintf("%v", id)
		if n, err := strconv.Atoi(s); err == nil && s != "" {
			return n
		}
		return s
	}
}

func (d *EmulatedLoupedeckDevice) TriggerButtonDown(id interface{}) {
	norm := normalizeTriggerID(id)
	d.Mu.RLock()
	cb := d.OnDown
	d.Mu.RUnlock()
	if cb != nil {
		cb(norm)
	}
}

func (d *EmulatedLoupedeckDevice) TriggerButtonUp(id interface{}) {
	norm := normalizeTriggerID(id)
	d.Mu.RLock()
	cb := d.OnUp
	d.Mu.RUnlock()
	if cb != nil {
		cb(norm)
	}
}

func (d *EmulatedLoupedeckDevice) TriggerRotate(id string, delta int) {
	if delta == 0 {
		delta = 1
	}
	d.Mu.RLock()
	cb := d.OnRotate
	d.Mu.RUnlock()
	if cb != nil {
		cb(id, delta)
	}
}

func (d *EmulatedLoupedeckDevice) TriggerTouchStart(screen string, key int, x, y float64) {
	tKey := 0
	if screen == "center" {
		tKey = key
	}
	tp := TouchPoint{
		X:      x,
		Y:      y,
		Target: TouchTarget{Screen: screen, Key: tKey},
	}
	d.Mu.RLock()
	cb := d.OnTouchStart
	d.Mu.RUnlock()
	if cb != nil {
		cb(TouchEventData{
			Touches:        []TouchPoint{tp},
			ChangedTouches: []TouchPoint{tp},
		})
	}
}

func (d *EmulatedLoupedeckDevice) TriggerTouchMove(screen string, key int, x, y float64) {
	tKey := 0
	if screen == "center" {
		tKey = key
	}
	tp := TouchPoint{
		X:      x,
		Y:      y,
		Target: TouchTarget{Screen: screen, Key: tKey},
	}
	d.Mu.RLock()
	cb := d.OnTouchMove
	d.Mu.RUnlock()
	if cb != nil {
		cb(TouchEventData{
			Touches:        []TouchPoint{tp},
			ChangedTouches: []TouchPoint{tp},
		})
	}
}

func (d *EmulatedLoupedeckDevice) TriggerTouchEnd(screen string, key int, x, y float64) {
	tKey := 0
	if screen == "center" {
		tKey = key
	}
	tp := TouchPoint{
		X:      x,
		Y:      y,
		Target: TouchTarget{Screen: screen, Key: tKey},
	}
	d.Mu.RLock()
	cb := d.OnTouchEnd
	d.Mu.RUnlock()
	if cb != nil {
		cb(TouchEventData{
			Touches:        []TouchPoint{},
			ChangedTouches: []TouchPoint{tp},
		})
	}
}

func (d *EmulatedLoupedeckDevice) GetSnapshot() map[string]interface{} {
	d.Mu.RLock()
	defer d.Mu.RUnlock()

	colorsCopy := make(map[string]string, len(d.ButtonColors))
	for k, v := range d.ButtonColors {
		colorsCopy[k] = v
	}
	screensCopy := make(map[string]interface{}, len(d.ScreenDataURL))
	for k, v := range d.ScreenDataURL {
		if v == "" {
			screensCopy[k] = nil
		} else {
			screensCopy[k] = v
		}
	}
	if _, ok := screensCopy["left"]; !ok {
		screensCopy["left"] = nil
	}
	if _, ok := screensCopy["right"]; !ok {
		screensCopy["right"] = nil
	}

	displaysMap := make(map[string]interface{}, len(d.Displays))
	for k, v := range d.Displays {
		displaysMap[k] = map[string]interface{}{
			"width":  v.Width,
			"height": v.Height,
		}
	}

	var vib interface{}
	if d.LastVibration != nil {
		vib = map[string]interface{}{
			"pattern":   d.LastVibration.Pattern,
			"timestamp": d.LastVibration.Timestamp,
		}
	}

	return map[string]interface{}{
		"type":          d.Type,
		"model":         d.Model,
		"connected":     d.Connected,
		"brightness":    d.Brightness,
		"lastVibration": vib,
		"buttonColors":  colorsCopy,
		"screens":       screensCopy,
		"displays":      displaysMap,
		"rows":          d.Rows,
		"columns":       d.Columns,
	}
}

