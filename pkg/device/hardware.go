package device

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

type DiscoveredPort struct {
	Path         string
	VendorID     uint16
	ProductID    uint16
	SerialNumber string
	Model        string
}

func DiscoverSerialDevices() ([]DiscoveredPort, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	var results []DiscoveredPort
	for _, p := range ports {
		if !p.IsUSB {
			continue
		}
		vid64, _ := strconv.ParseUint(p.VID, 16, 16)
		pid64, _ := strconv.ParseUint(p.PID, 16, 16)
		vid := uint16(vid64)
		pid := uint16(pid64)
		isMatch := false
		for _, v := range VENDOR_IDS {
			if vid == v {
				isMatch = true
				break
			}
		}
		if !isMatch {
			continue
		}
		model := "live"
		switch {
		case vid == 0x2ec2 && (pid == 0x0003 || pid == 0x0007):
			model = "ct"
		case vid == 0x2ec2 && pid == 0x0004:
			model = "live"
		case vid == 0x2ec2 && pid == 0x0006:
			model = "live-s"
		case vid == 0x1532 && pid == 0x0d06:
			model = "razer"
		case vid == 0x1532 && pid == 0x0d09:
			model = "razer-x"
		}
		results = append(results, DiscoveredPort{
			Path:         p.Name,
			VendorID:     vid,
			ProductID:    pid,
			SerialNumber: p.SerialNumber,
			Model:        model,
		})
	}
	return results, nil
}

type HardwareLoupedeckDevice struct {
	Mu            sync.RWMutex
	Path          string
	ProductID     uint16
	VendorID      uint16
	Type          string
	Model         string
	Rows          int
	Columns       int
	KeySize       int
	VisibleX      [2]int
	Displays      map[string]DisplayInfo
	Buttons       []interface{}
	Knobs         []string
	Canvases      map[string]*Canvas
	ScreenDataURL map[string]string
	ButtonColors  map[string]string
	Brightness    float64
	LastVibration *VibrationState

	portMu        sync.Mutex
	port          serial.Port
	connected     bool
	sendMu        sync.Mutex
	drawMu        sync.Mutex
	txID          byte
	pendingMu     sync.Mutex
	pending       map[byte]chan []byte
	touches       map[int]TouchPoint
	stopCh        chan struct{}
	eventCh       chan func()

	OnConnect     func(address string)
	OnDisconnect  func(err error)
	OnDown        func(id interface{})
	OnUp          func(id interface{})
	OnRotate      func(id string, delta int)
	OnTouchStart  func(data TouchEventData)
	OnTouchMove   func(data TouchEventData)
	OnTouchEnd    func(data TouchEventData)
}

func NewHardwareLoupedeckDevice(disc DiscoveredPort) *HardwareLoupedeckDevice {
	d := &HardwareLoupedeckDevice{
		Path:          disc.Path,
		VendorID:      disc.VendorID,
		ProductID:     disc.ProductID,
		Model:         disc.Model,
		Type:          "Loupedeck Live",
		Rows:          3,
		Columns:       4,
		KeySize:       90,
		VisibleX:      [2]int{0, 480},
		Brightness:    1.0,
		Displays: map[string]DisplayInfo{
			"center": {ID: []byte{0x00, 'M'}, Width: 360, Height: 270, Offset: [2]int{60, 0}},
			"left":   {ID: []byte{0x00, 'M'}, Width: 60, Height: 270, Offset: [2]int{0, 0}},
			"right":  {ID: []byte{0x00, 'M'}, Width: 60, Height: 270, Offset: [2]int{420, 0}},
		},
		Buttons:       []interface{}{0, 1, 2, 3, 4, 5, 6, 7},
		Knobs:         []string{"knobCL", "knobCR", "knobTL", "knobTR", "knobBL", "knobBR"},
		Canvases:      make(map[string]*Canvas),
		ScreenDataURL: make(map[string]string),
		ButtonColors:  make(map[string]string),
		pending:       make(map[byte]chan []byte),
		touches:       make(map[int]TouchPoint),
	}

	switch disc.Model {
	case "ct":
		d.Type = "Loupedeck CT"
		d.Buttons = []interface{}{0, 1, 2, 3, 4, 5, 6, 7, "home", "enter", "undo", "save", "keyboard", "fnL", "a", "b", "c", "d", "fnR", "e"}
		if disc.ProductID == 0x0007 {
			// CT v2
			d.Displays = map[string]DisplayInfo{
				"center": {ID: []byte{0x00, 'M'}, Width: 360, Height: 270, Offset: [2]int{60, 0}},
				"left":   {ID: []byte{0x00, 'M'}, Width: 60, Height: 270},
				"right":  {ID: []byte{0x00, 'M'}, Width: 60, Height: 270, Offset: [2]int{420, 0}},
				"knob":   {ID: []byte{0x00, 'W'}, Width: 240, Height: 240, Endianness: "be"},
			}
		} else {
			d.Displays = map[string]DisplayInfo{
				"center": {ID: []byte{0x00, 'A'}, Width: 360, Height: 270},
				"left":   {ID: []byte{0x00, 'L'}, Width: 60, Height: 270},
				"right":  {ID: []byte{0x00, 'R'}, Width: 60, Height: 270},
				"knob":   {ID: []byte{0x00, 'W'}, Width: 240, Height: 240, Endianness: "be"},
			}
		}
	case "live-s":
		d.Type = "Loupedeck Live S"
		d.Buttons = []interface{}{0, 1, 2, 3}
		d.Knobs = []string{"knobCL", "knobTL"}
		d.Columns = 5
		d.Rows = 3
		d.VisibleX = [2]int{15, 465}
		d.Displays = map[string]DisplayInfo{
			"center": {ID: []byte{0x00, 'M'}, Width: 480, Height: 270},
		}
	case "razer":
		d.Type = "Razer Stream Controller"
	case "razer-x":
		d.Type = "Razer Stream Controller X"
		d.Buttons = []interface{}{}
		d.Knobs = []string{}
		d.Columns = 5
		d.Rows = 3
		d.KeySize = 96
		d.VisibleX = [2]int{0, 480}
		d.Displays = map[string]DisplayInfo{
			"center": {ID: []byte{0x00, 'M'}, Width: 480, Height: 288},
		}
	}

	for name, dim := range d.Displays {
		c := NewCanvas(dim.Width, dim.Height)
		ctx := c.GetContext2D()
		ctx.FillStyle = "#000000"
		ctx.FillRect(0, 0, float64(dim.Width), float64(dim.Height))
		d.Canvases[name] = c
	}

	return d
}

func (d *HardwareLoupedeckDevice) GetType() string                     { return d.Type }
func (d *HardwareLoupedeckDevice) GetModel() string                    { return d.Model }
func (d *HardwareLoupedeckDevice) GetRows() int                        { return d.Rows }
func (d *HardwareLoupedeckDevice) GetColumns() int                     { return d.Columns }
func (d *HardwareLoupedeckDevice) GetKeySize() int                     { return d.KeySize }
func (d *HardwareLoupedeckDevice) GetVisibleX() [2]int                 { return d.VisibleX }
func (d *HardwareLoupedeckDevice) GetDisplays() map[string]DisplayInfo { return d.Displays }
func (d *HardwareLoupedeckDevice) GetButtons() []interface{}           { return d.Buttons }
func (d *HardwareLoupedeckDevice) GetKnobs() []string                  { return d.Knobs }

func (d *HardwareLoupedeckDevice) GetScreenDataURL(screenName string) string {
	d.Mu.Lock()
	defer d.Mu.Unlock()
	if url := d.ScreenDataURL[screenName]; url != "" {
		return url
	}
	if c, ok := d.Canvases[screenName]; ok && c != nil {
		url := c.ToDataURL()
		d.ScreenDataURL[screenName] = url
		return url
	}
	return ""
}

func performWSHandshake(port serial.Port, timeout time.Duration) ([]byte, bool) {
	_ = port.SetReadTimeout(timeout)
	_ = port.ResetInputBuffer()
	if _, err := port.Write([]byte(WS_UPGRADE_HEADER)); err != nil {
		return nil, false
	}

	deadline := time.Now().Add(timeout)
	var acc []byte
	chunk := make([]byte, 512)
	for time.Now().Before(deadline) {
		n, err := port.Read(chunk)
		if err != nil {
			break
		}
		if n == 0 {
			if len(acc) > 0 {
				break
			}
			continue
		}
		acc = append(acc, chunk[:n]...)
		if idx := bytes.Index(acc, []byte("\r\n\r\n")); idx != -1 {
			if strings.HasPrefix(string(acc), WS_UPGRADE_RESPONSE) {
				return acc[idx+4:], true
			}
			return nil, false
		}
		if idx := bytes.Index(acc, []byte("\n\n")); idx != -1 {
			if strings.HasPrefix(string(acc), WS_UPGRADE_RESPONSE) {
				return acc[idx+2:], true
			}
			return nil, false
		}
		if len(acc) >= len(WS_UPGRADE_RESPONSE) && !strings.HasPrefix(string(acc), WS_UPGRADE_RESPONSE) {
			return nil, false
		}
	}
	if strings.HasPrefix(string(acc), WS_UPGRADE_RESPONSE) {
		return nil, true
	}
	return nil, false
}

func (d *HardwareLoupedeckDevice) Connect() error {
	mode := &serial.Mode{
		BaudRate: 256000,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(d.Path, mode)
	if err != nil {
		return err
	}

	leftover, ok := performWSHandshake(port, 400*time.Millisecond)
	if !ok {
		// Device may still be in WebSocket mode from a previous session; send close frame to reset it
		_, _ = port.Write(WS_CLOSE_FRAME)
		time.Sleep(50 * time.Millisecond)
		_ = port.ResetInputBuffer()
		leftover, ok = performWSHandshake(port, 600*time.Millisecond)
	}
	if !ok {
		_, _ = port.Write(WS_CLOSE_FRAME)
		_ = port.Close()
		return fmt.Errorf("invalid Loupedeck handshake response on %s", d.Path)
	}

	_ = port.SetReadTimeout(100 * time.Millisecond)

	d.portMu.Lock()
	d.port = port
	d.connected = true
	d.stopCh = make(chan struct{})
	d.eventCh = make(chan func(), 256)
	stopCh := d.stopCh
	eventCh := d.eventCh
	d.portMu.Unlock()

	go d.eventLoop(stopCh, eventCh)
	go d.readLoop(port, stopCh, leftover)

	if d.OnConnect != nil {
		d.OnConnect(d.Path)
	}
	return nil
}

func (d *HardwareLoupedeckDevice) clearPending() {
	d.pendingMu.Lock()
	for tx, ch := range d.pending {
		delete(d.pending, tx)
		if ch != nil {
			close(ch)
		}
	}
	d.pendingMu.Unlock()
}

func (d *HardwareLoupedeckDevice) Close() error {
	d.portMu.Lock()
	if !d.connected || d.port == nil {
		d.portMu.Unlock()
		return nil
	}
	d.connected = false
	if d.stopCh != nil {
		close(d.stopCh)
		d.stopCh = nil
	}
	port := d.port
	d.port = nil
	_, _ = port.Write(WS_CLOSE_FRAME)
	err := port.Close()
	d.portMu.Unlock()

	d.clearPending()

	if d.OnDisconnect != nil {
		d.OnDisconnect(nil)
	}
	return err
}

func (d *HardwareLoupedeckDevice) eventLoop(stopCh chan struct{}, eventCh chan func()) {
	for {
		select {
		case <-stopCh:
			return
		case fn, ok := <-eventCh:
			if !ok {
				return
			}
			if fn != nil {
				fn()
			}
		}
	}
}

func (d *HardwareLoupedeckDevice) dispatchEvent(fn func()) {
	d.portMu.Lock()
	eventCh := d.eventCh
	stopCh := d.stopCh
	d.portMu.Unlock()
	if eventCh == nil || stopCh == nil {
		go fn()
		return
	}
	select {
	case <-stopCh:
	case eventCh <- fn:
	default:
		go fn()
	}
}

func (d *HardwareLoupedeckDevice) readLoop(port serial.Port, stopCh chan struct{}, initial []byte) {
	buffer := append([]byte(nil), initial...)
	readBuf := make([]byte, 1024)
	for {
		for {
			idx := bytes.IndexByte(buffer, 0x82)
			if idx == -1 {
				buffer = buffer[:0]
				break
			}
			if len(buffer) < idx+2 {
				if idx > 0 {
					buffer = buffer[idx:]
				}
				break
			}
			nextLen := int(buffer[idx+1])
			expectedEnd := idx + 2 + nextLen
			if len(buffer) < expectedEnd {
				if idx > 0 {
					buffer = buffer[idx:]
				}
				break
			}
			packet := make([]byte, nextLen)
			copy(packet, buffer[idx+2:expectedEnd])
			buffer = buffer[expectedEnd:]
			d.onReceive(packet)
		}

		select {
		case <-stopCh:
			return
		default:
		}
		n, err := port.Read(readBuf)
		if err != nil {
			d.portMu.Lock()
			wasConnected := d.connected
			d.connected = false
			if d.stopCh != nil {
				close(d.stopCh)
				d.stopCh = nil
			}
			d.port = nil
			d.portMu.Unlock()
			d.clearPending()
			if wasConnected && d.OnDisconnect != nil {
				d.OnDisconnect(err)
			}
			return
		}
		if n == 0 {
			continue
		}
		buffer = append(buffer, readBuf[:n]...)
	}
}

func (d *HardwareLoupedeckDevice) onReceive(buff []byte) {
	if len(buff) < 3 {
		return
	}
	msgLen := int(buff[0])
	if msgLen > len(buff) || msgLen < 3 {
		msgLen = len(buff)
	}
	cmd := buff[1]
	txID := buff[2]
	payload := append([]byte(nil), buff[3:msgLen]...)

	// Resolve pending transaction ACK immediately on readLoop before any callback
	d.pendingMu.Lock()
	ch, exists := d.pending[txID]
	if exists {
		delete(d.pending, txID)
	}
	d.pendingMu.Unlock()
	if exists && ch != nil {
		select {
		case ch <- payload:
		default:
		}
	}

	// Dispatch user input callbacks asynchronously via eventLoop so readLoop never blocks
	switch cmd {
	case CMD_BUTTON_PRESS:
		d.dispatchEvent(func() {
			d.handleButtonPress(payload)
		})
	case CMD_KNOB_ROTATE:
		d.dispatchEvent(func() {
			d.handleKnobRotate(payload)
		})
	case CMD_TOUCH, CMD_TOUCH_CT:
		d.dispatchEvent(func() {
			d.handleTouch("touchmove", payload)
		})
	case CMD_TOUCH_END, CMD_TOUCH_END_CT:
		d.dispatchEvent(func() {
			d.handleTouch("touchend", payload)
		})
	}
}

func (d *HardwareLoupedeckDevice) handleButtonPress(payload []byte) {
	if len(payload) < 2 {
		return
	}
	id, ok := BUTTONS[payload[0]]
	if !ok {
		return
	}
	isDown := payload[1] == 0x00
	if isDown {
		if d.OnDown != nil {
			d.OnDown(id)
		}
	} else {
		if d.OnUp != nil {
			d.OnUp(id)
		}
	}

	if d.Model == "razer-x" {
		if keyInt, ok := id.(int); ok {
			row := keyInt / d.Columns
			col := keyInt % d.Columns
			tp := TouchPoint{
				ID:     0,
				X:      (float64(col) + 0.5) * float64(d.KeySize),
				Y:      (float64(row) + 0.5) * float64(d.KeySize),
				Target: TouchTarget{Screen: "center", Key: keyInt},
			}
			if isDown && d.OnTouchStart != nil {
				d.OnTouchStart(TouchEventData{Touches: []TouchPoint{tp}, ChangedTouches: []TouchPoint{tp}})
			} else if !isDown && d.OnTouchEnd != nil {
				d.OnTouchEnd(TouchEventData{Touches: []TouchPoint{}, ChangedTouches: []TouchPoint{tp}})
			}
		}
	}
}

func (d *HardwareLoupedeckDevice) handleKnobRotate(payload []byte) {
	if len(payload) < 2 {
		return
	}
	id, ok := BUTTONS[payload[0]]
	if !ok {
		return
	}
	idStr := fmt.Sprintf("%v", id)
	delta := int(int8(payload[1]))
	if d.OnRotate != nil {
		d.OnRotate(idStr, delta)
	}
}

func (d *HardwareLoupedeckDevice) getTarget(x, y float64, id int) TouchTarget {
	if d.Model == "ct" && id == 0 {
		return TouchTarget{Screen: "knob", Key: 0}
	}
	if d.Model == "live-s" {
		if int(x) < d.VisibleX[0] || int(x) >= d.VisibleX[1] {
			return TouchTarget{}
		}
		col := (int(x) - d.VisibleX[0]) / d.KeySize
		row := int(y) / d.KeySize
		if col < 0 {
			col = 0
		} else if col >= d.Columns {
			col = d.Columns - 1
		}
		if row < 0 {
			row = 0
		} else if row >= d.Rows {
			row = d.Rows - 1
		}
		return TouchTarget{Screen: "center", Key: row*d.Columns + col}
	}
	leftWidth := 60
	centerWidth := 360
	if l, ok := d.Displays["left"]; ok {
		leftWidth = l.Width
	}
	if c, ok := d.Displays["center"]; ok {
		centerWidth = c.Width
	}
	if int(x) < leftWidth {
		return TouchTarget{Screen: "left", Key: 0}
	}
	if int(x) >= leftWidth+centerWidth {
		return TouchTarget{Screen: "right", Key: 0}
	}
	col := (int(x) - leftWidth) / d.KeySize
	row := int(y) / d.KeySize
	if col < 0 {
		col = 0
	} else if col >= d.Columns {
		col = d.Columns - 1
	}
	if row < 0 {
		row = 0
	} else if row >= d.Rows {
		row = d.Rows - 1
	}
	return TouchTarget{Screen: "center", Key: row*d.Columns + col}
}

func (d *HardwareLoupedeckDevice) handleTouch(event string, payload []byte) {
	if len(payload) < 6 {
		return
	}
	x := float64(binary.BigEndian.Uint16(payload[1:3]))
	y := float64(binary.BigEndian.Uint16(payload[3:5]))
	id := int(payload[5])
	target := d.getTarget(x, y, id)
	tp := TouchPoint{
		ID:     id,
		X:      x,
		Y:      y,
		Target: target,
	}

	d.Mu.Lock()
	if event == "touchend" {
		delete(d.touches, id)
	} else {
		if _, exists := d.touches[id]; !exists {
			event = "touchstart"
		}
		d.touches[id] = tp
	}
	touchesList := make([]TouchPoint, 0, len(d.touches))
	for _, t := range d.touches {
		touchesList = append(touchesList, t)
	}
	d.Mu.Unlock()

	evtData := TouchEventData{
		Touches:        touchesList,
		ChangedTouches: []TouchPoint{tp},
	}
	switch event {
	case "touchstart":
		if d.OnTouchStart != nil {
			d.OnTouchStart(evtData)
		}
	case "touchmove":
		if d.OnTouchMove != nil {
			d.OnTouchMove(evtData)
		}
	case "touchend":
		if d.OnTouchEnd != nil {
			d.OnTouchEnd(evtData)
		}
	}
}

func (d *HardwareLoupedeckDevice) sendPacket(cmd byte, data []byte) error {
	// Serialize all transactions on the wire so the Loupedeck MCU processes one packet + ACK at a time
	d.sendMu.Lock()
	defer d.sendMu.Unlock()

	d.portMu.Lock()
	if !d.connected || d.port == nil {
		d.portMu.Unlock()
		return fmt.Errorf("device not connected")
	}
	stopCh := d.stopCh
	d.txID++
	if d.txID == 0 {
		d.txID = 1
	}
	txID := d.txID

	ch := make(chan []byte, 1)
	d.pendingMu.Lock()
	d.pending[txID] = ch
	d.pendingMu.Unlock()

	headerLen := 3 + len(data)
	if headerLen > 0xff {
		headerLen = 0xff
	}
	packetLen := 3 + len(data)
	var prepLen int
	if packetLen > 0xff {
		prepLen = 14
	} else {
		prepLen = 6
	}

	frame := make([]byte, prepLen+packetLen)
	if packetLen > 0xff {
		frame[0] = 0x82
		frame[1] = 0xff
		binary.BigEndian.PutUint32(frame[6:10], uint32(packetLen))
	} else {
		frame[0] = 0x82
		frame[1] = byte(0x80 + packetLen)
	}
	packet := frame[prepLen:]
	packet[0] = byte(headerLen)
	packet[1] = cmd
	packet[2] = txID
	copy(packet[3:], data)

	written := 0
	var writeErr error
	for written < len(frame) {
		n, err := d.port.Write(frame[written:])
		if err != nil {
			writeErr = err
			break
		}
		if n <= 0 {
			writeErr = fmt.Errorf("short write on serial port")
			break
		}
		written += n
	}

	if writeErr != nil {
		wasConnected := d.connected
		d.connected = false
		if d.stopCh != nil {
			close(d.stopCh)
			d.stopCh = nil
		}
		if d.port != nil {
			_ = d.port.Close()
			d.port = nil
		}
		d.portMu.Unlock()
		d.clearPending()
		if wasConnected && d.OnDisconnect != nil {
			go d.OnDisconnect(writeErr)
		}
		return writeErr
	}
	d.portMu.Unlock()

	ackTimeout := 150 * time.Millisecond
	if cmd == CMD_FRAMEBUFF {
		ackTimeout = 500 * time.Millisecond
	}
	timer := time.NewTimer(ackTimeout)
	defer timer.Stop()

	select {
	case <-ch:
		return nil
	case <-stopCh:
		d.pendingMu.Lock()
		delete(d.pending, txID)
		d.pendingMu.Unlock()
		return fmt.Errorf("device disconnected")
	case <-timer.C:
		d.pendingMu.Lock()
		delete(d.pending, txID)
		d.pendingMu.Unlock()
		return nil
	}
}

func (d *HardwareLoupedeckDevice) SetBrightness(value float64) error {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	d.Mu.Lock()
	d.Brightness = value
	d.Mu.Unlock()
	b := byte(math.Max(0, math.Min(MAX_BRIGHTNESS, math.Round(value*MAX_BRIGHTNESS))))
	return d.sendPacket(CMD_SET_BRIGHTNESS, []byte{b})
}

func (d *HardwareLoupedeckDevice) Vibrate(pattern byte) error {
	if d.Model == "razer-x" {
		return nil
	}
	if pattern == 0 {
		pattern = HAPTIC["SHORT"]
	}
	d.Mu.Lock()
	d.LastVibration = &VibrationState{
		Pattern:   pattern,
		Timestamp: time.Now().UnixMilli(),
	}
	d.Mu.Unlock()
	return d.sendPacket(CMD_SET_VIBRATION, []byte{pattern})
}

func (d *HardwareLoupedeckDevice) SetButtonColor(id interface{}, colorStr string) error {
	if d.Model == "razer-x" {
		return nil
	}
	key, ok := FindButtonByte(id)
	if !ok {
		return fmt.Errorf("invalid button ID: %v", id)
	}
	r, g, b := config.ColorToRGBBytes(colorStr)
	d.Mu.Lock()
	d.ButtonColors[fmt.Sprintf("%v", id)] = colorStr
	d.Mu.Unlock()
	return d.sendPacket(CMD_SET_COLOR, []byte{key, r, g, b})
}

func (d *HardwareLoupedeckDevice) DrawScreen(screenName string, drawCallback func(ctx *Context2D) error) error {
	// Keep canvas render + CMD_FRAMEBUFF + CMD_DRAW atomic per screen draw so concurrent callers never interleave
	d.drawMu.Lock()
	defer d.drawMu.Unlock()

	d.Mu.Lock()
	displayInfo, hasDisplay := d.Displays[screenName]
	canvas, hasCanvas := d.Canvases[screenName]
	if !hasDisplay || !hasCanvas {
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
	d.ScreenDataURL[screenName] = ""
	buf := canvas.ToRGB565(displayInfo.Endianness == "be")
	d.Mu.Unlock()

	x := displayInfo.Offset[0]
	y := displayInfo.Offset[1]
	w := displayInfo.Width
	h := displayInfo.Height

	header := make([]byte, 8)
	binary.BigEndian.PutUint16(header[0:2], uint16(x))
	binary.BigEndian.PutUint16(header[2:4], uint16(y))
	binary.BigEndian.PutUint16(header[4:6], uint16(w))
	binary.BigEndian.PutUint16(header[6:8], uint16(h))

	fbData := make([]byte, 0, len(displayInfo.ID)+len(header)+len(buf))
	fbData = append(fbData, displayInfo.ID...)
	fbData = append(fbData, header...)
	fbData = append(fbData, buf...)

	if err := d.sendPacket(CMD_FRAMEBUFF, fbData); err != nil {
		return err
	}
	return d.sendPacket(CMD_DRAW, displayInfo.ID)
}

