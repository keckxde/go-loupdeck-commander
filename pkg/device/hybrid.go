package device

import (
	"fmt"
	"strings"
	"sync"
)

// HybridLoupedeckDevice composes an EmulatedLoupedeckDevice with an optional
// physical Hardware Device. Capability queries reflect the attached hardware
// when present (falling back to the emulated device), while output commands
// (brightness, vibration, button colors, and screen draws) are mirrored to both.
// Access to the underlying device references is synchronized via Mu.
type HybridLoupedeckDevice struct {
	Mu       sync.RWMutex
	Emulated *EmulatedLoupedeckDevice
	Hardware Device
}

var _ Device = (*HybridLoupedeckDevice)(nil)

// NewHybridLoupedeckDevice creates a HybridLoupedeckDevice pairing emulated
// with an optional physical hardware device. If emulated is nil, a default
// "live" EmulatedLoupedeckDevice is created.
func NewHybridLoupedeckDevice(emulated *EmulatedLoupedeckDevice, hardware Device) *HybridLoupedeckDevice {
	if emulated == nil {
		emulated = NewEmulatedLoupedeckDevice(EmulatedDeviceOptions{Model: "live"})
	}
	return &HybridLoupedeckDevice{
		Emulated: emulated,
		Hardware: hardware,
	}
}

// SetHardwareDevice attaches or clears the physical hardware Device under Mu.
func (d *HybridLoupedeckDevice) SetHardwareDevice(hw Device) {
	d.Mu.Lock()
	d.Hardware = hw
	d.Mu.Unlock()
}

// GetHardwareDevice returns the currently attached physical hardware Device, or nil.
func (d *HybridLoupedeckDevice) GetHardwareDevice() Device {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Hardware
}

// HasHardwareDevice reports whether a physical hardware Device is currently attached.
func (d *HybridLoupedeckDevice) HasHardwareDevice() bool {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Hardware != nil
}

// SetEmulatedDevice replaces the underlying EmulatedLoupedeckDevice under Mu.
func (d *HybridLoupedeckDevice) SetEmulatedDevice(emu *EmulatedLoupedeckDevice) {
	d.Mu.Lock()
	d.Emulated = emu
	d.Mu.Unlock()
}

// GetEmulatedDevice returns the underlying EmulatedLoupedeckDevice under Mu.
func (d *HybridLoupedeckDevice) GetEmulatedDevice() *EmulatedLoupedeckDevice {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Emulated
}

// snapshotDevices returns a consistent read-locked snapshot of the emulated
// and hardware device pointers so callers never hold Mu during device I/O.
func (d *HybridLoupedeckDevice) snapshotDevices() (*EmulatedLoupedeckDevice, Device) {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Emulated, d.Hardware
}

// GetType returns a combined device type description indicating whether
// physical hardware is currently attached alongside the emulated controller.
func (d *HybridLoupedeckDevice) GetType() string {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return fmt.Sprintf("%s (Physical + Emulated)", hw.GetType())
	}
	if emu == nil {
		return "Loupedeck (Hybrid - Waiting for Hardware)"
	}
	base := strings.TrimSuffix(emu.GetType(), " (Emulated)")
	return fmt.Sprintf("%s (Hybrid - Waiting for Hardware)", base)
}

// GetModel returns the model identifier from the attached hardware device, or
// from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetModel() string {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetModel()
	}
	if emu != nil {
		return emu.GetModel()
	}
	return ""
}

// GetRows returns the touch key row count from the attached hardware device,
// or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetRows() int {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetRows()
	}
	if emu != nil {
		return emu.GetRows()
	}
	return 0
}

// GetColumns returns the touch key column count from the attached hardware
// device, or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetColumns() int {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetColumns()
	}
	if emu != nil {
		return emu.GetColumns()
	}
	return 0
}

// GetKeySize returns the touch key pixel size from the attached hardware
// device, or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetKeySize() int {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetKeySize()
	}
	if emu != nil {
		return emu.GetKeySize()
	}
	return 0
}

// GetVisibleX returns the visible horizontal pixel range [minX, maxX] from the
// attached hardware device, or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetVisibleX() [2]int {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetVisibleX()
	}
	if emu != nil {
		return emu.GetVisibleX()
	}
	return [2]int{}
}

// GetDisplays returns the display descriptors from the attached hardware
// device, or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetDisplays() map[string]DisplayInfo {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetDisplays()
	}
	if emu != nil {
		return emu.GetDisplays()
	}
	return nil
}

// GetButtons returns the physical button identifiers from the attached hardware
// device, or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetButtons() []interface{} {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetButtons()
	}
	if emu != nil {
		return emu.GetButtons()
	}
	return nil
}

// GetKnobs returns the rotary knob identifiers from the attached hardware
// device, or from the emulated device if no hardware is attached.
func (d *HybridLoupedeckDevice) GetKnobs() []string {
	emu, hw := d.snapshotDevices()
	if hw != nil {
		return hw.GetKnobs()
	}
	if emu != nil {
		return emu.GetKnobs()
	}
	return nil
}

// Connect connects the emulated device and, if present, connects the physical
// hardware device and returns any hardware connection error.
func (d *HybridLoupedeckDevice) Connect() error {
	emu, hw := d.snapshotDevices()
	if emu != nil {
		_ = emu.Connect()
	}
	if hw != nil {
		return hw.Connect()
	}
	return nil
}

// Close closes the emulated device and, if present, closes the physical
// hardware device and returns any hardware close error.
func (d *HybridLoupedeckDevice) Close() error {
	emu, hw := d.snapshotDevices()
	if emu != nil {
		_ = emu.Close()
	}
	if hw != nil {
		return hw.Close()
	}
	return nil
}

// SetBrightness mirrors the normalized brightness value to both the emulated
// device and any attached hardware device.
func (d *HybridLoupedeckDevice) SetBrightness(value float64) error {
	emu, hw := d.snapshotDevices()
	if emu != nil {
		_ = emu.SetBrightness(value)
	}
	if hw != nil {
		_ = hw.SetBrightness(value)
	}
	return nil
}

// Vibrate mirrors the haptic pattern command to both the emulated device and
// any attached hardware device.
func (d *HybridLoupedeckDevice) Vibrate(pattern byte) error {
	emu, hw := d.snapshotDevices()
	if emu != nil {
		_ = emu.Vibrate(pattern)
	}
	if hw != nil {
		_ = hw.Vibrate(pattern)
	}
	return nil
}

// SetButtonColor mirrors the button LED color update to both the emulated
// device and any attached hardware device.
func (d *HybridLoupedeckDevice) SetButtonColor(id interface{}, color string) error {
	emu, hw := d.snapshotDevices()
	if emu != nil {
		_ = emu.SetButtonColor(id, color)
	}
	if hw != nil {
		_ = hw.SetButtonColor(id, color)
	}
	return nil
}

// DrawScreen invokes drawCallback to render screenName on both the emulated
// device and any attached hardware device.
func (d *HybridLoupedeckDevice) DrawScreen(screenName string, drawCallback func(ctx *Context2D) error) error {
	emu, hw := d.snapshotDevices()
	if emu != nil {
		_ = emu.DrawScreen(screenName, drawCallback)
	}
	if hw != nil {
		_ = hw.DrawScreen(screenName, drawCallback)
	}
	return nil
}

// GetScreenDataURL returns the PNG data URL for screenName from the emulated
// device, or an empty string if no emulated device is configured.
func (d *HybridLoupedeckDevice) GetScreenDataURL(screenName string) string {
	emu, _ := d.snapshotDevices()
	if emu != nil {
		return emu.GetScreenDataURL(screenName)
	}
	return ""
}
