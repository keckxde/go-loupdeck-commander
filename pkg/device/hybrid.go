package device

import (
	"fmt"
	"strings"
	"sync"
)

type HybridLoupedeckDevice struct {
	Mu       sync.RWMutex
	Emulated *EmulatedLoupedeckDevice
	Hardware Device
}

var _ Device = (*HybridLoupedeckDevice)(nil)

func NewHybridLoupedeckDevice(emulated *EmulatedLoupedeckDevice, hardware Device) *HybridLoupedeckDevice {
	if emulated == nil {
		emulated = NewEmulatedLoupedeckDevice(EmulatedDeviceOptions{Model: "live"})
	}
	return &HybridLoupedeckDevice{
		Emulated: emulated,
		Hardware: hardware,
	}
}

func (d *HybridLoupedeckDevice) SetHardwareDevice(hw Device) {
	d.Mu.Lock()
	d.Hardware = hw
	d.Mu.Unlock()
}

func (d *HybridLoupedeckDevice) GetHardwareDevice() Device {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Hardware
}

func (d *HybridLoupedeckDevice) HasHardwareDevice() bool {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Hardware != nil
}

func (d *HybridLoupedeckDevice) SetEmulatedDevice(emu *EmulatedLoupedeckDevice) {
	d.Mu.Lock()
	d.Emulated = emu
	d.Mu.Unlock()
}

func (d *HybridLoupedeckDevice) GetEmulatedDevice() *EmulatedLoupedeckDevice {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Emulated
}

func (d *HybridLoupedeckDevice) snapshotDevices() (*EmulatedLoupedeckDevice, Device) {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	return d.Emulated, d.Hardware
}

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

func (d *HybridLoupedeckDevice) GetScreenDataURL(screenName string) string {
	emu, _ := d.snapshotDevices()
	if emu != nil {
		return emu.GetScreenDataURL(screenName)
	}
	return ""
}
