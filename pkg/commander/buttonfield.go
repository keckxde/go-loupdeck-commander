package commander

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/device"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/interfaces"
)

// ButtonFieldOptions configures cell dimensions and drawing offsets for a
// ButtonField.
type ButtonFieldOptions struct {
	CellWidth  float64
	CellHeight float64
	OffsetX    float64
	OffsetY    float64
}

// ButtonField groups a grid or collection of Button instances belonging to a
// single Loupedeck surface (physical buttons, knobs, or touch screens).
type ButtonField struct {
	Mu         sync.RWMutex
	Name       string
	Rows       int
	Columns    int
	Width      int
	Height     int
	CellWidth  float64
	CellHeight float64
	OffsetX    float64
	OffsetY    float64
	Screen     bool
	Type       string
	Profile    *config.Profile
	Buttons    map[string]*Button
	Keys       []string
	Dirty      bool
	Ifaces     *interfaces.Manager
}

// NewButtonField creates a ButtonField and initializes Button instances for all
// keys present in data using the computed cell dimensions and options.
func NewButtonField(name string, rows, columns, width, height int, data map[string]interface{}, profile *config.Profile, opts ButtonFieldOptions, ifaces *interfaces.Manager) *ButtonField {
	screen := width > 0 && height > 0
	bType := "button"
	if screen {
		bType = "touch"
	}

	defaultCellWidth := float64(width)
	if columns > 0 {
		defaultCellWidth = float64(width) / float64(columns)
	}
	defaultCellHeight := float64(height)
	if rows > 0 {
		defaultCellHeight = float64(height) / float64(rows)
	}
	cellWidth := defaultCellWidth
	if opts.CellWidth > 0 {
		cellWidth = opts.CellWidth
	}
	cellHeight := defaultCellHeight
	if opts.CellHeight > 0 {
		cellHeight = opts.CellHeight
	}

	bf := &ButtonField{
		Name:       name,
		Rows:       rows,
		Columns:    columns,
		Width:      width,
		Height:     height,
		CellWidth:  cellWidth,
		CellHeight: cellHeight,
		OffsetX:    opts.OffsetX,
		OffsetY:    opts.OffsetY,
		Screen:     screen,
		Type:       bType,
		Profile:    profile,
		Buttons:    make(map[string]*Button),
		Keys:       []string{},
		Dirty:      true,
		Ifaces:     ifaces,
	}

	if data != nil {
		keys := config.OrderedKeys(data)
		for _, k := range keys {
			bData, _ := data[k].(map[string]interface{})
			tb := NewButton(fmt.Sprintf("%s-%s", bType, k), cellWidth, cellHeight, bData, k, profile, opts.OffsetX, opts.OffsetY, ifaces)
			bf.Buttons[k] = tb
		}
		bf.Keys = keys
	}

	return bf
}

// IsDirty reports whether any button in the field requires redrawing.
func (bf *ButtonField) IsDirty() bool {
	bf.Mu.RLock()
	defer bf.Mu.RUnlock()
	return bf.Dirty
}

// MarkDirty flags the ButtonField as needing a redraw.
func (bf *ButtonField) MarkDirty() {
	bf.Mu.Lock()
	defer bf.Mu.Unlock()
	bf.Dirty = true
}

// HasBlinkingButtons reports whether any button in the field is currently
// blinking.
func (bf *ButtonField) HasBlinkingButtons() bool {
	bf.Mu.RLock()
	defer bf.Mu.RUnlock()
	for _, k := range bf.Keys {
		if btn := bf.Buttons[k]; btn != nil && btn.IsBlinking() {
			return true
		}
	}
	for _, btn := range bf.Buttons {
		if btn != nil && btn.IsBlinking() {
			return true
		}
	}
	return false
}

// MapHardwareKeyToConfigKey maps a hardware key index from a device grid of
// size (deviceColumns, deviceRows) onto the ButtonField's configured grid.
func (bf *ButtonField) MapHardwareKeyToConfigKey(hardwareKey interface{}, deviceColumns, deviceRows int) (string, bool) {
	bf.Mu.RLock()
	defer bf.Mu.RUnlock()

	hwStr := config.ToString(hardwareKey)
	if !bf.Screen || (bf.Columns <= 1 && bf.Rows <= 1) {
		return hwStr, true
	}
	hwNum, err := strconv.Atoi(strings.TrimSpace(hwStr))
	if err != nil || hwNum < 0 {
		return hwStr, true
	}
	devCols := deviceColumns
	if devCols <= 0 {
		devCols = bf.Columns
	}
	devRows := deviceRows
	if devRows <= 0 {
		devRows = bf.Rows
	}
	if devCols == bf.Columns && devRows == bf.Rows {
		return strconv.Itoa(hwNum), true
	}
	row := hwNum / devCols
	col := hwNum % devCols
	if row >= bf.Rows || col >= bf.Columns {
		return "", false
	}
	return strconv.Itoa(row*bf.Columns + col), true
}

// Draw renders either physical button LED colors or the full touchscreen grid
// onto dev, re-marking the field dirty if drawing fails.
func (bf *ButtonField) Draw(dev device.Device) error {
	if dev == nil {
		return nil
	}
	bf.Mu.Lock()
	bf.Dirty = false
	keys := append([]string(nil), bf.Keys...)
	isScreen := bf.Screen
	cols := bf.Columns
	rows := bf.Rows
	width := bf.Width
	height := bf.Height
	name := bf.Name
	bf.Mu.Unlock()

	if !isScreen {
		supported := dev.GetButtons()
		for _, key := range keys {
			if len(supported) > 0 && !isPhysicalButtonSupported(supported, key) {
				continue
			}
			bf.Mu.RLock()
			btn := bf.Buttons[key]
			bf.Mu.RUnlock()
			if btn != nil {
				if err := btn.DrawPhysical(dev, key); err != nil {
					bf.MarkDirty()
					return err
				}
			}
		}
		return nil
	}

	gridCols := cols
	if gridCols <= 0 {
		gridCols = dev.GetColumns()
		if gridCols <= 0 {
			gridCols = 1
		}
	}
	gridRows := rows
	if gridRows <= 0 {
		gridRows = dev.GetRows()
		if gridRows <= 0 {
			gridRows = 1
		}
	}

	err := dev.DrawScreen(name, func(ctx *device.Context2D) error {
		ctx.GlobalCompositeOperation = "source-over"
		ctx.FillStyle = "#000000"
		ctx.FillRect(0, 0, float64(width), float64(height))
		ctx.GlobalCompositeOperation = "source-atop"
		for _, key := range keys {
			iVal, err := strconv.Atoi(key)
			if err != nil {
				continue
			}
			r := iVal / gridCols
			c := iVal % gridCols
			if r < 0 || r >= gridRows || c < 0 || c >= gridCols {
				continue
			}
			bf.Mu.RLock()
			btn := bf.Buttons[key]
			bf.Mu.RUnlock()
			if btn != nil {
				btn.Draw(r, c, ctx)
			}
		}
		return nil
	})
	if err != nil {
		bf.MarkDirty()
		return err
	}
	return nil
}

// isPhysicalButtonSupported reports whether key matches any string or integer
// button identifier in supported.
func isPhysicalButtonSupported(supported []interface{}, key string) bool {
	numKey, numErr := strconv.Atoi(key)
	for _, s := range supported {
		switch v := s.(type) {
		case string:
			if v == key {
				return true
			}
		case int:
			if numErr == nil && v == numKey {
				return true
			}
		}
	}
	return false
}

// SyncProfileButtons sets the state ("on"/"off") of buttons belonging to the
// "profiles" group to reflect activeProfileIndex.
func (bf *ButtonField) SyncProfileButtons(activeProfileIndex int) {
	if activeProfileIndex < 0 {
		return
	}
	bf.Mu.Lock()
	defer bf.Mu.Unlock()

	for _, key := range bf.Keys {
		btn := bf.Buttons[key]
		if btn == nil || btn.GetGroup() != "profiles" {
			continue
		}
		targetProfile, hasTarget := btn.GetTargetProfile()
		if !hasTarget {
			if kInt, err := strconv.Atoi(key); err == nil && kInt >= 1 {
				targetProfile = kInt - 1
				hasTarget = true
			}
		}
		if !hasTarget {
			continue
		}
		if targetProfile == activeProfileIndex {
			btn.SetState("on")
		} else {
			btn.SetState("off")
		}
	}
	bf.Dirty = true
}

// SetState sets the state of button id to val and marks the field dirty if the
// button exists.
func (bf *ButtonField) SetState(id string, val interface{}) {
	bf.Mu.Lock()
	btn := bf.Buttons[id]
	if btn != nil {
		bf.Dirty = true
	}
	bf.Mu.Unlock()
	if btn != nil {
		btn.SetState(val)
	}
}

// GetSnapshot returns a map of button keys to their current GetSnapshot outputs.
func (bf *ButtonField) GetSnapshot() map[string]interface{} {
	bf.Mu.RLock()
	defer bf.Mu.RUnlock()
	out := make(map[string]interface{}, len(bf.Keys))
	for _, k := range bf.Keys {
		if btn := bf.Buttons[k]; btn != nil {
			out[k] = btn.GetSnapshot()
		}
	}
	return out
}

// Load triggers image preloading on every button in the field and marks the
// field dirty.
func (bf *ButtonField) Load() {
	bf.Mu.RLock()
	keys := append([]string(nil), bf.Keys...)
	prof := bf.Profile
	bf.Mu.RUnlock()

	for _, k := range keys {
		bf.Mu.RLock()
		btn := bf.Buttons[k]
		bf.Mu.RUnlock()
		if btn != nil {
			btn.Load(prof)
		}
	}
	bf.MarkDirty()
}

// checkAndCreateButton returns the Button for id, creating a default Button if
// one is not already registered.
func (bf *ButtonField) checkAndCreateButton(id string) *Button {
	bf.Mu.Lock()
	defer bf.Mu.Unlock()
	if btn, exists := bf.Buttons[id]; exists && btn != nil {
		return btn
	}
	tb := NewButton(id, 1, 1, nil, id, bf.Profile, 0, 0, bf.Ifaces)
	bf.Buttons[id] = tb
	return tb
}

// Pressed forwards a press event to button id and marks the field dirty.
func (bf *ButtonField) Pressed(id string) bool {
	btn := bf.checkAndCreateButton(id)
	res := btn.Pressed()
	bf.MarkDirty()
	return res
}

// Released forwards a release event to button id, resets peer buttons sharing
// its toggle group if any, and marks the field dirty.
func (bf *ButtonField) Released(id string) bool {
	btn := bf.checkAndCreateButton(id)
	res := btn.Released()
	bf.Mu.Lock()
	bf.Dirty = true
	keys := append([]string(nil), bf.Keys...)
	bf.Mu.Unlock()

	if res {
		toggleGroup := btn.GetGroup()
		if toggleGroup != "" {
			for _, key := range keys {
				if id == key {
					continue
				}
				bf.Mu.RLock()
				other := bf.Buttons[key]
				bf.Mu.RUnlock()
				if other != nil && other.GetGroup() == toggleGroup {
					other.SetState(0)
				}
			}
		}
	}
	return res
}

// ButtonStateChanged propagates an external state or attribute update for
// changedButtonID and reports whether a matching button was updated.
func (bf *ButtonField) ButtonStateChanged(changedButtonID, attribute, nodeid string, val interface{}) bool {
	bf.Mu.RLock()
	keys := append([]string(nil), bf.Keys...)
	bf.Mu.RUnlock()

	updated := false
	for i, buttonID := range keys {
		if changedButtonID == buttonID {
			bf.Mu.RLock()
			btn := bf.Buttons[buttonID]
			bf.Mu.RUnlock()
			if btn != nil {
				btn.ButtonStateChanged(i, attribute, nodeid, val)
				bf.MarkDirty()
				updated = true
			}
		}
	}
	return updated
}

// Rotated forwards a rotary encoder delta event to button id and marks the
// field dirty.
func (bf *ButtonField) Rotated(id string, delta int) bool {
	btn := bf.checkAndCreateButton(id)
	res := btn.Rotated(delta)
	bf.MarkDirty()
	return res
}

// TouchMove forwards a touch-move coordinate event to button id and marks the
// field dirty.
func (bf *ButtonField) TouchMove(id string, x, y float64) bool {
	btn := bf.checkAndCreateButton(id)
	res := btn.TouchMove(x, y)
	bf.MarkDirty()
	return res
}
