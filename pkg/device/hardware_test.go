package device

import (
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"
)

type mockSerialPort struct {
	mu      sync.Mutex
	inPipeR *io.PipeReader
	inPipeW *io.PipeWriter
	closed  bool

	cmdOrderMu sync.Mutex
	cmdOrder   []byte
}

func newMockSerialPort() *mockSerialPort {
	r, w := io.Pipe()
	return &mockSerialPort{
		inPipeR: r,
		inPipeW: w,
	}
}

func (m *mockSerialPort) SetMode(mode *serial.Mode) error               { return nil }
func (m *mockSerialPort) Read(p []byte) (int, error)                    { return m.inPipeR.Read(p) }
func (m *mockSerialPort) Drain() error                                  { return nil }
func (m *mockSerialPort) ResetInputBuffer() error                       { return nil }
func (m *mockSerialPort) ResetOutputBuffer() error                      { return nil }
func (m *mockSerialPort) SetDTR(dtr bool) error                         { return nil }
func (m *mockSerialPort) SetRTS(rts bool) error                         { return nil }
func (m *mockSerialPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (m *mockSerialPort) SetReadTimeout(t time.Duration) error { return nil }
func (m *mockSerialPort) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		_ = m.inPipeW.Close()
		_ = m.inPipeR.Close()
	}
	return nil
}
func (m *mockSerialPort) Break(d time.Duration) error { return nil }

func (m *mockSerialPort) Write(p []byte) (int, error) {
	// Parse outgoing WebSocket frame header to extract cmd and txID and send hardware ACK
	var cmd, txID byte
	if len(p) >= 9 && p[0] == 0x82 {
		if p[1] == 0xff && len(p) >= 17 {
			packetLen := int(binary.BigEndian.Uint32(p[6:10]))
			if len(p) == 14+packetLen {
				cmd = p[15]
				txID = p[16]
			}
		} else if p[1] >= 0x80 {
			packetLen := int(p[1] - 0x80)
			if len(p) == 6+packetLen {
				cmd = p[7]
				txID = p[8]
			}
		}
	}
	if txID != 0 {
		m.cmdOrderMu.Lock()
		m.cmdOrder = append(m.cmdOrder, cmd)
		m.cmdOrderMu.Unlock()

		ack := []byte{0x82, 0x03, 0x03, cmd, txID}
		go func() {
			_, _ = m.inPipeW.Write(ack)
		}()
	}
	return len(p), nil
}

func TestHardwareDeviceFlowControlAndNoReadLoopDeadlock(t *testing.T) {
	dev := NewHardwareLoupedeckDevice(DiscoveredPort{
		Path:      "/dev/mockACM0",
		VendorID:  0x2ec2,
		ProductID: 0x0004,
		Model:     "live",
	})
	port := newMockSerialPort()
	dev.port = port
	dev.connected = true
	dev.stopCh = make(chan struct{})
	dev.eventCh = make(chan func(), 256)

	go dev.eventLoop(dev.stopCh, dev.eventCh)
	go dev.readLoop(port, dev.stopCh, nil)
	defer func() { _ = dev.Close() }()

	// 1. Trigger a button press from hardware whose callback draws screens synchronously.
	// Previously, if sendPacket waited for ACKs while onReceive called OnDown on readLoop, this deadlocked.
	doneCh := make(chan error, 1)
	dev.OnDown = func(id interface{}) {
		if err := dev.DrawScreen("center", func(ctx *Context2D) error {
			ctx.FillStyle = "#00ff00"
			ctx.FillRect(0, 0, 90, 90)
			return nil
		}); err != nil {
			doneCh <- err
			return
		}
		doneCh <- nil
	}

	// Simulate physical button 1 (0x08) press packet from device
	_, _ = port.inPipeW.Write([]byte{0x82, 0x05, 0x05, CMD_BUTTON_PRESS, 0x00, 0x08, 0x00})

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("expected DrawScreen inside OnDown to succeed, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for OnDown -> DrawScreen (readLoop deadlocked!)")
	}

	// 2. Verify concurrent DrawScreen calls strictly pair CMD_FRAMEBUFF followed immediately by CMD_DRAW
	port.cmdOrderMu.Lock()
	port.cmdOrder = nil
	port.cmdOrderMu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			screen := "center"
			if idx%3 == 1 {
				screen = "left"
			} else if idx%3 == 2 {
				screen = "right"
			}
			_ = dev.DrawScreen(screen, func(ctx *Context2D) error {
				ctx.FillStyle = "#336699"
				ctx.FillRect(0, 0, 60, 60)
				return nil
			})
		}(i)
	}
	wg.Wait()

	port.cmdOrderMu.Lock()
	cmds := append([]byte(nil), port.cmdOrder...)
	port.cmdOrderMu.Unlock()

	if len(cmds) != 12 {
		t.Fatalf("expected 12 commands (6*(CMD_FRAMEBUFF+CMD_DRAW)), got %d: %v", len(cmds), cmds)
	}
	for i := 0; i < len(cmds); i += 2 {
		if cmds[i] != CMD_FRAMEBUFF || cmds[i+1] != CMD_DRAW {
			t.Fatalf("interleaved DrawScreen commands at pair %d: got (0x%02x, 0x%02x), want (CMD_FRAMEBUFF=0x10, CMD_DRAW=0x0f)", i/2, cmds[i], cmds[i+1])
		}
	}
}
