package qdp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

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
	ctx               context.Context
	cancel            context.CancelFunc
	reader            *MessageReader // Add new field
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
	ctx, cancel := context.WithCancel(context.Background())
	t := &FTDITransport{
		config:            config,
		connectionHandler: connectionHandler,
		ctx:               ctx,
		cancel:            cancel,
	}

	if err := t.connect(gousb.NewContext()); err != nil {
		cancel()
		return nil, err
	}

	// Initialize reader after successful connection
	t.reader = NewMessageReader(&endpointReader{
		ep:  t.inEndpoint,
		ctx: t.ctx,
	})

	return t, nil
}

// Add new method to handle connection setup
func (t *FTDITransport) connect(ctx *gousb.Context) error {
	if ctx == nil {
		return errors.New("USB context is nil")
	}

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

	// Add additional validation for endpoints
	if inEndpoint == nil || outEndpoint == nil {
		intf.Close()
		cfg.Close()
		device.Close()
		ctx.Close()
		return errors.New("failed to get valid endpoints")
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

	// Reset reader after reconnection
	t.reader = NewMessageReader(&endpointReader{
		ep:  t.inEndpoint,
		ctx: t.ctx,
		buf: make([]byte, ftdiMaxPacketSize),
	})

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
	// Wait a bit before attempting reconnection
	time.Sleep(100 * time.Millisecond)

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
	if t.reader == nil {
		return nil, errors.New("transport not initialized")
	}

	msg, err := t.reader.ReadMessage()

	if err != nil {
		if isUsbDisconnectError(err) && !t.disconnected.Load() {
			t.disconnected.Store(true)
			if t.connectionHandler != nil {
				t.connectionHandler.OnDisconnect(t, fmt.Errorf("USB device disconnected: %v", err))
			}
		}

		// Try reconnecting if we hit any error
		if err := t.tryReconnect(); err != nil {
			return nil, fmt.Errorf("reconnect failed: %v", err)
		}

		// Retry reading after successful reconnection
		return t.reader.ReadMessage()
	}

	// Clear disconnected flag if we successfully read
	if t.disconnected.Load() {
		t.disconnected.Store(false)
		if t.connectionHandler != nil {
			t.connectionHandler.OnConnect(t)
		}
	}

	return msg, nil
}

func (t *FTDITransport) WriteMessage(msg *Message) error {
	// Try writing first
	encoded, err := msg.Encode()
	if err != nil {
		return err
	}

	_, err = writeChunked(t.outEndpoint, t.ctx, encoded)
	if err != nil {
		if isUsbDisconnectError(err) && !t.disconnected.Load() {
			t.disconnected.Store(true)
			if t.connectionHandler != nil {
				t.connectionHandler.OnDisconnect(t, fmt.Errorf("USB device disconnected: %v", err))
			}
		}

		// Try reconnecting
		if err := t.tryReconnect(); err != nil {
			return fmt.Errorf("reconnect failed: %v", err)
		}

		// Retry writing after successful reconnection
		_, err = writeChunked(t.outEndpoint, t.ctx, encoded)
		if err != nil {
			return err
		}
	}

	// Clear disconnected flag if we successfully wrote
	if t.disconnected.Load() {
		t.disconnected.Store(false)
		if t.connectionHandler != nil {
			t.connectionHandler.OnConnect(t)
		}
	}

	return nil
}

func (t *FTDITransport) Close() error {
	// Cancel context first to interrupt any pending reads/writes
	t.cancel()

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
	return strings.Contains(strings.ToLower(errStr), "no such device") ||
		strings.Contains(strings.ToLower(errStr), "device not responding") ||
		strings.Contains(strings.ToLower(errStr), "device not found") ||
		strings.Contains(strings.ToLower(errStr), "device disconnected") ||
		strings.Contains(strings.ToLower(errStr), "endpoint not found") ||
		strings.Contains(strings.ToLower(errStr), "pipe error") ||
		strings.Contains(strings.ToLower(errStr), "operation timed out") ||
		strings.Contains(strings.ToLower(errStr), "resource temporarily unavailable")
}

const (
	ftdiStatusSize    = 2  // Size of FTDI status bytes
	ftdiMaxPacketSize = 64 // Maximum USB packet size including status bytes
	ftdiMaxDataSize   = 62 // Maximum data size per packet (64 - 2 status bytes)
)

type ioReaderWithContext interface {
	ReadContext(ctx context.Context, p []byte) (int, error)
}

// endpointReader implements io.Reader for USB endpoints
type endpointReader struct {
	ep   ioReaderWithContext
	ctx  context.Context
	buf  []byte
	pos  int
	size int
}

func (r *endpointReader) Read(p []byte) (n int, err error) {
	if r.ep == nil {
		return 0, errors.New("endpoint is nil")
	}
	if len(p) == 0 {
		return 0, nil
	}

	// If buffer is empty, read new packet
	if r.pos >= r.size {
		if r.buf == nil {
			r.buf = make([]byte, ftdiMaxPacketSize)
		}

		// Read with context and handle errors
		bytesRead, err := r.ep.ReadContext(r.ctx, r.buf)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return 0, err
			}
			// For USB errors, return EOF to trigger reconnect
			return 0, io.EOF
		}

		// Strip status bytes and store data
		if bytesRead > ftdiStatusSize {
			r.size = bytesRead - ftdiStatusSize
			r.pos = 0
		} else {
			r.size = 0
			r.pos = 0
			return 0, nil
		}
	}

	// Copy data from buffer to output
	n = copy(p, r.buf[ftdiStatusSize+r.pos:ftdiStatusSize+r.size])
	r.pos += n
	return n, nil
}

// Add new method to handle chunked writes for FTDI
func writeChunked(ep *gousb.OutEndpoint, ctx context.Context, data []byte) (int, error) {
	var written int
	for len(data) > 0 {
		// Calculate chunk size
		chunkSize := ftdiMaxDataSize
		if len(data) < chunkSize {
			chunkSize = len(data)
		}

		// Write chunk
		n, err := ep.WriteContext(ctx, data[:chunkSize])
		if err != nil {
			return written, err
		}
		written += n
		data = data[chunkSize:]
	}
	return written, nil
}
