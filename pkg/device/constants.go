// Package device provides abstractions, serial/WebSocket protocol constants,
// 2D canvas rendering, and physical, emulated, and hybrid implementations for
// Loupedeck and Razer Stream Controller devices.
package device

import "encoding/binary"

// BUTTONS maps raw hardware button and knob byte identifiers to logical
// button indices (int) or named control strings, covering Loupedeck devices
// and the Razer Stream Controller X.
var BUTTONS = map[byte]interface{}{
	0x00: "knobCT",
	0x01: "knobTL",
	0x02: "knobCL",
	0x03: "knobBL",
	0x04: "knobTR",
	0x05: "knobCR",
	0x06: "knobBR",
	0x07: 0,
	0x08: 1,
	0x09: 2,
	0x0a: 3,
	0x0b: 4,
	0x0c: 5,
	0x0d: 6,
	0x0e: 7,
	0x0f: "home",
	0x10: "undo",
	0x11: "keyboard",
	0x12: "enter",
	0x13: "save",
	0x14: "fnL",
	0x15: "a",
	0x16: "c",
	0x17: "fnR",
	0x18: "b",
	0x19: "d",
	0x1a: "e",
	// Razer Stream Controller X
	0x1b: 0,
	0x1c: 1,
	0x1d: 2,
	0x1e: 3,
	0x1f: 4,
	0x20: 5,
	0x21: 6,
	0x22: 7,
	0x23: 8,
	0x24: 9,
	0x25: 10,
	0x26: 11,
	0x27: 12,
	0x28: 13,
	0x29: 14,
}

// FindButtonByte resolves a logical button identifier (int or string) to its
// hardware protocol byte code and reports whether a matching entry exists.
// Standard Loupedeck codes (0x00–0x1a) are preferred before Razer Stream
// Controller X codes (0x1b–0x29).
func FindButtonByte(id interface{}) (byte, bool) {
	// Prefer 0x00..0x1a before 0x1b..0x29
	for b := byte(0x00); b <= 0x1a; b++ {
		if v, ok := BUTTONS[b]; ok && v == id {
			return b, true
		}
	}
	for b := byte(0x1b); b <= 0x29; b++ {
		if v, ok := BUTTONS[b]; ok && v == id {
			return b, true
		}
	}
	return 0, false
}

// Hardware serial WebSocket protocol command identifiers exchanged with
// Loupedeck and Razer Stream Controller devices.
const (
	CMD_BUTTON_PRESS   byte = 0x00
	CMD_KNOB_ROTATE    byte = 0x01
	CMD_SET_COLOR      byte = 0x02
	CMD_SERIAL         byte = 0x03
	CMD_RESET          byte = 0x06
	CMD_VERSION        byte = 0x07
	CMD_SET_BRIGHTNESS byte = 0x09
	CMD_MCU            byte = 0x0d
	CMD_DRAW           byte = 0x0f
	CMD_FRAMEBUFF      byte = 0x10
	CMD_SET_VIBRATION  byte = 0x1b
	CMD_TOUCH          byte = 0x4d
	CMD_TOUCH_CT       byte = 0x52
	CMD_TOUCH_END      byte = 0x6d
	CMD_TOUCH_END_CT   byte = 0x72
)

// HAPTIC maps named haptic feedback patterns to their hardware vibration
// waveform byte codes.
var HAPTIC = map[string]byte{
	"SHORT":        0x01,
	"MEDIUM":       0x0a,
	"LONG":         0x0f,
	"LOW":          0x31,
	"SHORT_LOW":    0x32,
	"SHORT_LOWER":  0x33,
	"LOWER":        0x40,
	"LOWEST":       0x41,
	"DESCEND_SLOW": 0x46,
	"DESCEND_MED":  0x47,
	"DESCEND_FAST": 0x48,
	"ASCEND_SLOW":  0x52,
	"ASCEND_MED":   0x53,
	"ASCEND_FAST":  0x58,
	"REV_SLOWEST":  0x5e,
	"REV_SLOW":     0x5f,
	"REV_MED":      0x60,
	"REV_FAST":     0x61,
	"REV_FASTER":   0x62,
	"REV_FASTEST":  0x63,
	"RISE_FALL":    0x6a,
	"BUZZ":         0x70,
	"VERY_LONG":    0x76,
	"RUMBLE5":      0x77,
	"RUMBLE4":      0x78,
	"RUMBLE3":      0x79,
	"RUMBLE2":      0x7a,
	"RUMBLE1":      0x7b,
}

// Device brightness scaling, reconnect defaults, and WebSocket upgrade
// handshake strings.
const (
	MAX_BRIGHTNESS             = 10
	DEFAULT_RECONNECT_INTERVAL = 3000
	WS_UPGRADE_HEADER          = "GET /index.html\nHTTP/1.1\nConnection: Upgrade\nUpgrade: websocket\nSec-WebSocket-Key: 123abc\n\n"
	WS_UPGRADE_RESPONSE        = "HTTP/1.1"
)

// WS_CLOSE_FRAME is a masked WebSocket close frame sent to reset or cleanly
// close a hardware serial WebSocket session.
var WS_CLOSE_FRAME = []byte{0x88, 0x80, 0x00, 0x00, 0x00, 0x00}

// VENDOR_IDS lists USB vendor IDs recognized during serial device discovery.
var VENDOR_IDS = []uint16{
	0x2ec2, // Loupedeck
	0x1532, // Razer
}

// Rgba2Rgb565 converts pixelCount pixels from an RGBA byte slice into packed
// 16-bit RGB565 bytes. Pixels are encoded in big-endian byte order when
// bigEndian is true and little-endian otherwise.
func Rgba2Rgb565(rgba []byte, pixelCount int, bigEndian bool) []byte {
	out := make([]byte, pixelCount*2)
	for i := 0; i < pixelCount*4 && i+2 < len(rgba); i += 4 {
		r := uint16(rgba[i])
		g := uint16(rgba[i+1])
		b := uint16(rgba[i+2])
		color := (b >> 3) | ((g & 0xfc) << 3) | ((r & 0xf8) << 8)
		if bigEndian {
			binary.BigEndian.PutUint16(out[i/2:], color)
		} else {
			binary.LittleEndian.PutUint16(out[i/2:], color)
		}
	}
	return out
}

// DisplayInfo describes a physical or emulated screen region, including its
// hardware display ID, pixel dimensions, framebuffer offset, and RGB565 byte order.
type DisplayInfo struct {
	ID         []byte `json:"-"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Offset     [2]int `json:"offset,omitempty"`
	Endianness string `json:"endianness,omitempty"`
}

// TouchTarget identifies the screen name and key index targeted by a touch contact.
type TouchTarget struct {
	Screen string `json:"screen"`
	Key    int    `json:"key"`
}

// TouchPoint represents a single touch contact with its tracking ID,
// screen-space coordinates, and resolved TouchTarget.
type TouchPoint struct {
	ID     int         `json:"id"`
	X      float64     `json:"x"`
	Y      float64     `json:"y"`
	Target TouchTarget `json:"target"`
}

// TouchEventData carries the active and changed touch contacts for touch event callbacks.
type TouchEventData struct {
	Touches        []TouchPoint `json:"touches"`
	ChangedTouches []TouchPoint `json:"changedTouches"`
}

// VibrationState records the most recently triggered haptic pattern byte and its
// Unix millisecond timestamp.
type VibrationState struct {
	Pattern   byte  `json:"pattern"`
	Timestamp int64 `json:"timestamp"`
}

// Device defines the unified interface implemented by physical, emulated, and
// hybrid Loupedeck and Razer Stream Controller devices.
type Device interface {
	GetType() string
	GetModel() string
	GetRows() int
	GetColumns() int
	GetKeySize() int
	GetVisibleX() [2]int
	GetDisplays() map[string]DisplayInfo
	GetButtons() []interface{}
	GetKnobs() []string
	Connect() error
	Close() error
	SetBrightness(value float64) error
	Vibrate(pattern byte) error
	SetButtonColor(id interface{}, color string) error
	DrawScreen(screenName string, drawCallback func(ctx *Context2D) error) error
	GetScreenDataURL(screenName string) string
}
