package qdp

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/google/gousb"
)

// FTDITransport implements ITransport for FTDI USB devices
type FTDITransport struct {
	context           *gousb.Context
	device            *gousb.Device
	config            *gousb.Config
	intf              *gousb.Interface // Add interface tracking
	inEndpoint        *gousb.InEndpoint
	outEndpoint       *gousb.OutEndpoint
	connectionHandler IConnectionHandler
	disconnected      atomic.Bool
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
	ctx := gousb.NewContext()

	device, err := ctx.OpenDeviceWithVIDPID(gousb.ID(config.VID), gousb.ID(config.PID))
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	if device == nil {
		ctx.Close()
		return nil, fmt.Errorf("device not found")
	}

	// Set auto detach for kernel drivers
	if err := device.SetAutoDetach(true); err != nil {
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to set auto detach: %w", err)
	}

	// Get default configuration
	cfg, err := device.Config(1)
	if err != nil {
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	// Claim interface
	intf, err := cfg.Interface(config.Interface, 0)
	if err != nil {
		cfg.Close()
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to claim interface: %w", err)
	}

	// Get endpoints
	inEndpoint, err := intf.InEndpoint(config.ReadEP)
	if err != nil {
		intf.Close()
		cfg.Close()
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to get IN endpoint: %w", err)
	}

	outEndpoint, err := intf.OutEndpoint(config.WriteEP)
	if err != nil {
		intf.Close()
		cfg.Close()
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to get OUT endpoint: %w", err)
	}

	// Configure UART settings
	if _, err := device.Control(
		0x40, // vendor request out
		0x03, // SET_BAUDRATE
		uint16(config.BaudRate),
		0,
		nil,
	); err != nil {
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to set baud rate: %w", err)
	}

	// Configure line properties (data bits, stop bits, parity)
	lineParams := uint16(config.DataBits) |
		(uint16(config.StopBits) << 11) |
		(uint16(config.Parity) << 8)

	if _, err := device.Control(
		0x40, // vendor request out
		0x04, // SET_LINE_PROPERTY
		lineParams,
		0,
		nil,
	); err != nil {
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to set line properties: %w", err)
	}

	// Configure flow control
	if _, err := device.Control(
		0x40,
		0x02, // SET_FLOW_CTRL
		uint16(config.FlowControl),
		0,
		nil,
	); err != nil {
		device.Close()
		ctx.Close()
		return nil, fmt.Errorf("failed to set flow control: %w", err)
	}

	t := &FTDITransport{
		context:           ctx,
		device:            device,
		config:            cfg,
		intf:              intf, // Store interface
		inEndpoint:        inEndpoint,
		outEndpoint:       outEndpoint,
		connectionHandler: connectionHandler,
	}

	if connectionHandler != nil {
		connectionHandler.OnConnect(t)
	}

	return t, nil
}

func (t *FTDITransport) ReadMessage() (*Message, error) {
	reader := &endpointReader{t.inEndpoint}

	msg, err := readMessage(reader)
	if err != nil {
		if isUsbDisconnectError(err) && !t.disconnected.Load() {
			// Handle USB disconnection
			t.disconnected.Store(true)
			if t.connectionHandler != nil {
				t.connectionHandler.OnDisconnect(t, fmt.Errorf("USB device disconnected: %v", err))
			}
		} else if isFatalError(err) && !t.disconnected.Load() {
			// Handle other fatal errors
			t.disconnected.Store(true)
			if t.connectionHandler != nil {
				t.connectionHandler.OnDisconnect(t, err)
			}
		}
		// Return the error regardless of whether it's fatal or not
		return nil, err
	}
	return msg, nil
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

// isFatalError determines if an error should cause a disconnect
func isFatalError(err error) bool {
	if err == nil {
		return false
	}

	// I/O errors are fatal
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return true
	}

	// USB specific errors are fatal
	if err == gousb.ErrorTimeout || err == gousb.ErrorNoDevice || err == gousb.ErrorBusy {
		return true
	}

	// Message format errors are non-fatal
	if err == ErrInvalidMessage || err == ErrCRCMismatch {
		return false
	}
	if strings.Contains(err.Error(), "invalid topic length") ||
		strings.Contains(err.Error(), "message too large") ||
		strings.Contains(err.Error(), "CRC mismatch") {
		return false
	}

	// Consider other I/O errors fatal
	if _, ok := err.(*os.PathError); ok {
		return true
	}

	// By default, treat unknown errors as non-fatal
	return false
}

func (t *FTDITransport) WriteMessage(msg *Message) error {
	encoded, err := msg.Encode()
	if err != nil {
		return err
	}

	_, err = t.outEndpoint.Write(encoded)
	if err != nil && isUsbDisconnectError(err) && !t.disconnected.Load() {
		t.disconnected.Store(true)
		if t.connectionHandler != nil {
			t.connectionHandler.OnDisconnect(t, fmt.Errorf("USB device disconnected: %v", err))
		}
	}
	return err
}

func (t *FTDITransport) Close() error {
	var err error

	// Close in reverse order of creation
	if t.intf != nil {
		t.intf.Close()
		t.intf = nil
	}

	if t.config != nil {
		if cfgErr := t.config.Close(); cfgErr != nil {
			err = cfgErr
		}
		t.config = nil
	}

	if t.device != nil {
		if devErr := t.device.Close(); devErr != nil && err == nil {
			err = devErr
		}
		t.device = nil
	}

	if t.context != nil {
		if ctxErr := t.context.Close(); ctxErr != nil && err == nil {
			err = ctxErr
		}
		t.context = nil
	}

	if t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
		t.connectionHandler.OnDisconnect(t, err)
	}

	return err
}

// endpointReader implements io.Reader for USB endpoints
type endpointReader struct {
	ep *gousb.InEndpoint
}

func (r *endpointReader) Read(p []byte) (n int, err error) {
	return r.ep.Read(p)
}
