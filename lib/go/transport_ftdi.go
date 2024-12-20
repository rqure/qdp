package qdp

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/google/gousb"
)

// FTDITransport implements ITransport for FTDI USB devices
type FTDITransport struct {
	context           *gousb.Context
	device            *gousb.Device
	usbConfig         *gousb.Config
	intf              *gousb.Interface
	inEndpoint        *gousb.InEndpoint
	outEndpoint       *gousb.OutEndpoint
	connectionHandler IConnectionHandler
	disconnected      atomic.Bool
	config            FTDIConfig // Store configuration for reconnect
}

// FTDIConfig holds configuration for FTDI transport
type FTDIConfig struct {
	VID         uint16 // Vendor ID
	PID         uint16 // Product ID
	Interface   int    // Interface number
	ReadEP      int    // Read endpoint number
	WriteEP     int    // Write endpoint number
	BaudRate    int    // Baud rate for UART
	DataBits    int    // Data bits (5, 6, 7, 8)
	StopBits    int    // Stop bits (1, 2)
	Parity      int    // Parity (0=none, 1=odd, 2=even)
	FlowControl int    // Flow control (0=none, 1=hardware)
}

// DefaultFTDIConfig provides default configuration for common FTDI devices
var DefaultFTDIConfig = FTDIConfig{
	VID:         0x0403, // FTDI default VID
	PID:         0x6001, // FT232R default PID
	Interface:   0,
	ReadEP:      0x81, // EP1 IN
	WriteEP:     0x01, // EP1 OUT
	BaudRate:    115200,
	DataBits:    8,
	StopBits:    1,
	Parity:      0,
	FlowControl: 0,
}

func NewFTDITransport(config FTDIConfig, connectionHandler IConnectionHandler) (*FTDITransport, error) {
	t := &FTDITransport{
		config:            config,
		connectionHandler: connectionHandler,
	}

	if err := t.connect(gousb.NewContext()); err != nil {
		return nil, err
	}

	return t, nil
}

// Add new method to handle connection setup
func (t *FTDITransport) connect(ctx *gousb.Context) error {
	device, err := ctx.OpenDeviceWithVIDPID(gousb.ID(t.config.VID), gousb.ID(t.config.PID))
	if err != nil {
		ctx.Close()
		return fmt.Errorf("failed to open device: %w", err)
	}

	if device == nil {
		ctx.Close()
		return fmt.Errorf("device not found")
	}

	// Set auto detach for kernel drivers
	if err := device.SetAutoDetach(true); err != nil {
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to set auto detach: %w", err)
	}

	// Get default configuration
	cfg, err := device.Config(1)
	if err != nil {
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Claim interface
	intf, err := cfg.Interface(t.config.Interface, 0)
	if err != nil {
		cfg.Close()
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to claim interface: %w", err)
	}

	// Get endpoints
	inEndpoint, err := intf.InEndpoint(t.config.ReadEP)
	if err != nil {
		intf.Close()
		cfg.Close()
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to get IN endpoint: %w", err)
	}

	outEndpoint, err := intf.OutEndpoint(t.config.WriteEP)
	if err != nil {
		intf.Close()
		cfg.Close()
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to get OUT endpoint: %w", err)
	}

	// Configure UART settings
	valueL, valueH := calculateFTDIBaudRateDivisor(t.config.BaudRate)

	// Set divisor low byte
	if _, err := device.Control(
		0x40,   // vendor request out
		0x03,   // SET_BAUDRATE
		valueL, // Divisor low byte
		valueH, // Divisor high byte
		nil,
	); err != nil {
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to set baud rate: %w", err)
	}

	// Configure line properties (data bits, stop bits, parity)
	lineParams := uint16(t.config.DataBits) |
		(uint16(t.config.StopBits) << 11) |
		(uint16(t.config.Parity) << 8)

	if _, err := device.Control(
		0x40, // vendor request out
		0x04, // SET_LINE_PROPERTY
		lineParams,
		0,
		nil,
	); err != nil {
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to set line properties: %w", err)
	}

	// Configure flow control
	if _, err := device.Control(
		0x40,
		0x02, // SET_FLOW_CTRL
		uint16(t.config.FlowControl),
		0,
		nil,
	); err != nil {
		device.Close()
		ctx.Close()
		return fmt.Errorf("failed to set flow control: %w", err)
	}

	t.context = ctx
	t.device = device
	t.usbConfig = cfg // Use renamed field
	t.intf = intf
	t.inEndpoint = inEndpoint
	t.outEndpoint = outEndpoint

	t.disconnected.Store(false)
	if t.connectionHandler != nil {
		t.connectionHandler.OnConnect(t)
	}

	return nil
}

func calculateFTDIBaudRateDivisor(baudRate int) (uint16, uint16) {
	// FTDI uses a 3 MHz reference clock
	const baseClock = 3000000

	// Calculate divisor
	divisor := float64(baseClock) / float64(baudRate)

	// Extract integer and sub-integer parts
	integerPart := int(divisor)
	subIntegerPart := divisor - float64(integerPart)

	// Map sub-integer parts to closest achievable values
	var prescaler int
	switch {
	case subIntegerPart < 0.0625:
		prescaler = 0
	case subIntegerPart < 0.1875:
		prescaler = 0x1000
	case subIntegerPart < 0.3125:
		prescaler = 0x2000
	case subIntegerPart < 0.4375:
		prescaler = 0x3000
	case subIntegerPart < 0.5625:
		prescaler = 0x4000
	case subIntegerPart < 0.6875:
		prescaler = 0x5000
	case subIntegerPart < 0.8125:
		prescaler = 0x6000
	case subIntegerPart < 0.9375:
		prescaler = 0x7000
	default:
		integerPart++
		prescaler = 0
	}

	// Combine integer part and prescaler
	finalDivisor := (integerPart & 0x3FFF) | prescaler

	// Split into high and low bytes
	valueH := uint16((finalDivisor >> 8) & 0xFF)
	valueL := uint16(finalDivisor & 0xFF)

	return valueL, valueH
}

// Add new method to attempt reconnection
func (t *FTDITransport) tryReconnect() error {
	// Clean up old connection first
	t.closeResources()

	// Try to establish new connection
	ctx := gousb.NewContext()
	if err := t.connect(ctx); err != nil {
		ctx.Close()
		return err
	}

	return nil
}

func (t *FTDITransport) closeResources() {
	if t.intf != nil {
		t.intf.Close()
		t.intf = nil
	}
	if t.usbConfig != nil { // Use renamed field
		t.usbConfig.Close()
		t.usbConfig = nil
	}
	if t.device != nil {
		t.device.Close()
		t.device = nil
	}
	if t.context != nil {
		t.context.Close()
		t.context = nil
	}
}

func (t *FTDITransport) ReadMessage() (*Message, error) {
	reader := &endpointReader{t.inEndpoint}
	msg, err := readMessage(reader)

	if err != nil {
		if isUsbDisconnectError(err) && !t.disconnected.Load() {
			t.disconnected.Store(true)
			if t.connectionHandler != nil {
				t.connectionHandler.OnDisconnect(t, fmt.Errorf("USB device disconnected: %v", err))
				// Try to reconnect on next read
				return nil, err
			}
		} else if t.disconnected.Load() {
			// Try reconnecting if we're disconnected
			if err := t.tryReconnect(); err == nil {
				// Retry reading after successful reconnection
				return t.ReadMessage()
			}
		}
		return nil, err
	}

	// If we got here after being disconnected, we're reconnected
	if t.disconnected.Load() {
		t.disconnected.Store(false)
		if t.connectionHandler != nil {
			t.connectionHandler.OnConnect(t)
		}
	}

	return msg, nil
}

func (t *FTDITransport) WriteMessage(msg *Message) error {
	encoded, err := msg.Encode()
	if err != nil {
		return err
	}

	_, err = t.outEndpoint.Write(encoded)
	if err != nil {
		if isUsbDisconnectError(err) && !t.disconnected.Load() {
			t.disconnected.Store(true)
			if t.connectionHandler != nil {
				t.connectionHandler.OnDisconnect(t, fmt.Errorf("USB device disconnected: %v", err))
			}
		} else if t.disconnected.Load() {
			// Try reconnecting if we're disconnected
			if err := t.tryReconnect(); err == nil {
				// Retry writing after successful reconnection
				return t.WriteMessage(msg)
			}
		}
	}

	return err
}

func (t *FTDITransport) Close() error {
	if t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
		t.connectionHandler.OnDisconnect(t, nil)
	}

	t.closeResources()
	return nil
}

// Add new helper function to detect USB disconnection errors
func isUsbDisconnectError(err error) bool {
	if err == nil {
		return false
	}

	// Common USB disconnection error patterns
	if err == gousb.ErrorNoDevice || err == gousb.ErrorNotFound {
		return true
	}

	errStr := err.Error()
	return strings.Contains(errStr, "no such device") ||
		strings.Contains(errStr, "device not responding") ||
		strings.Contains(errStr, "device disconnected") ||
		strings.Contains(errStr, "endpoint not found") ||
		strings.Contains(errStr, "pipe error") ||
		strings.Contains(errStr, "operation timed out")
}

// endpointReader implements io.Reader for USB endpoints
type endpointReader struct {
	ep *gousb.InEndpoint
}

func (r *endpointReader) Read(p []byte) (n int, err error) {
	return r.ep.Read(p)
}
