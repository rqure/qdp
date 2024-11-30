package qdp

import (
	"fmt"
	"io"
	"sync/atomic"

	"github.com/google/gousb"
)

// FTDITransport implements ITransport for FTDI USB devices
type FTDITransport struct {
	context           *gousb.Context
	device            *gousb.Device
	config            *gousb.Config
	inEndpoint        *gousb.InEndpoint
	outEndpoint       *gousb.OutEndpoint
	connectionHandler IConnectionHandler
	disconnected      atomic.Bool
}

// FTDIConfig holds configuration for FTDI transport
type FTDIConfig struct {
	VID       uint16 // Vendor ID
	PID       uint16 // Product ID
	Interface int    // Interface number
	ReadEP    int    // Read endpoint number
	WriteEP   int    // Write endpoint number
}

// DefaultFTDIConfig provides default configuration for common FTDI devices
var DefaultFTDIConfig = FTDIConfig{
	VID:       0x0403, // FTDI default VID
	PID:       0x6001, // FT232R default PID
	Interface: 0,
	ReadEP:    0x81, // EP1 IN
	WriteEP:   0x01, // EP1 OUT
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

	t := &FTDITransport{
		context:           ctx,
		device:            device,
		config:            cfg,
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
	// Buffer to read header (8 bytes: topic length + payload length)
	header := make([]byte, 8)
	_, err := io.ReadFull(&endpointReader{t.inEndpoint}, header)
	if err != nil {
		if err != io.EOF && t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
			t.connectionHandler.OnDisconnect(t, err)
		}
		return nil, err
	}

	// Read message using helper function
	msg, err := readMessage(&endpointReader{t.inEndpoint})
	if err != nil && t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
		t.connectionHandler.OnDisconnect(t, err)
	}
	return msg, err
}

func (t *FTDITransport) WriteMessage(msg *Message) error {
	encoded, err := msg.Encode()
	if err != nil {
		return err
	}

	_, err = t.outEndpoint.Write(encoded)
	return err
}

func (t *FTDITransport) Close() error {
	var err error
	if t.config != nil {
		err = t.config.Close()
	}
	if t.device != nil {
		if devErr := t.device.Close(); devErr != nil && err == nil {
			err = devErr
		}
	}
	if t.context != nil {
		if ctxErr := t.context.Close(); ctxErr != nil && err == nil {
			err = ctxErr
		}
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
