package bacnet

import (
	"context"
	"fmt"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"

	"github.com/Nortech-ai/bacNetIP/encoding"
)

const maxReattempt = 2
const readMultiReceiveTimeout = 5 * time.Second

// ReadMultiProperty uses the given device and read property request to read
// from a device. Along with being able to read multiple properties from a
// device, it can also read these properties from multiple objects. This is a
// good feature to read all present values of every object in the device. This
// is a batch operation compared to a ReadProperty and should be used in place
// when reading more than two objects/properties.
func (c *client) ReadMultiProperty(device btypes.Device, rp btypes.MultiplePropertyData) (btypes.MultiplePropertyData, error) {
	return c.ReadMultiPropertyContext(context.Background(), device, rp)
}

// ReadMultiPropertyContext performs ReadMultiProperty with caller-provided context.
func (c *client) ReadMultiPropertyContext(ctx context.Context, device btypes.Device, rp btypes.MultiplePropertyData) (btypes.MultiplePropertyData, error) {
	var out btypes.MultiplePropertyData

	if ctx == nil {
		ctx = context.Background()
	}

	idCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		idCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	id, err := c.tsm.ID(idCtx)
	if err != nil {
		return out, fmt.Errorf("unable to get transaction id: %w", err)
	}
	defer c.tsm.Put(id)
	device.Addr.SetLength()
	if err := c.tsm.ExpectSource(id, &device.Addr); err != nil {
		return out, fmt.Errorf("unable to set response source for transaction id %d: %w", id, err)
	}
	err = device.CheckADPU()
	if err != nil {
		return btypes.MultiplePropertyData{}, fmt.Errorf("check adpu: %w", err)
	}

	npdu := &btypes.NPDU{
		Version:               btypes.ProtocolVersion,
		Destination:           &device.Addr,
		Source:                c.dataLink.GetMyAddress(),
		IsNetworkLayerMessage: false,
		ExpectingReply:        true,
		Priority:              btypes.Normal,
		HopCount:              btypes.DefaultHopCount,
	}

	enc := encoding.NewEncoder()
	enc.NPDU(npdu)
	err = enc.ReadMultipleProperty(uint8(id), rp)
	if enc.Error() != nil || err != nil {
		return out, fmt.Errorf("encoding read multiple property failed: %w", err)
	}

	pack := enc.Bytes()
	if device.MaxApdu < uint32(len(pack)) {
		return out, fmt.Errorf("read multiple property is too large (max: %d given: %d)", device.MaxApdu, len(pack))
	}

	var lastErr error
	for range maxReattempt {
		if err := ctx.Err(); err != nil {
			return out, fmt.Errorf("read multiple property canceled: %w", err)
		}

		out, err = c.sendReadMultipleProperty(ctx, id, device, npdu, pack)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("read multiple property failed after %d retries", maxReattempt)
	}
	return out, fmt.Errorf("failed %d tries: %w", maxReattempt, lastErr)
}

func (c *client) sendReadMultipleProperty(ctx context.Context, id int, dev btypes.Device, npdu *btypes.NPDU, request []byte) (btypes.MultiplePropertyData, error) {
	var out btypes.MultiplePropertyData
	_, err := c.Send(dev.Addr, npdu, request, nil)
	if err != nil {
		return out, fmt.Errorf("send read multiple property request: %w", err)
	}

	timeout := receiveTimeoutFromContext(ctx, readMultiReceiveTimeout)
	raw, err := c.tsm.Receive(id, timeout)
	if err != nil {
		return out, fmt.Errorf("unable to receive id %d: %w", id, err)
	}

	var b []byte
	switch v := raw.(type) {
	case error:
		return out, v
	case []byte:
		b = v
	default:
		return out, fmt.Errorf("received unknown datatype %T", raw)
	}

	dec := encoding.NewDecoder(b)

	var apdu btypes.APDU
	if err = dec.APDU(&apdu); err != nil {
		return out, fmt.Errorf("decode apdu: %w", err)
	}
	if apdu.Error.Class != 0 || apdu.Error.Code != 0 {
		err = fmt.Errorf("received error, class: %d, code: %d", apdu.Error.Class, apdu.Error.Code)
		return out, err
	}
	err = dec.ReadMultiplePropertyAck(&out)
	if err != nil {
		c.log.Debugf("WEIRD PACKET: %v: %v", err, b)
		return out, fmt.Errorf("decode read multiple property ack: %w", err)
	}
	return out, err
}

func receiveTimeoutFromContext(ctx context.Context, defaultTimeout time.Duration) time.Duration {
	if ctx == nil {
		return defaultTimeout
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return defaultTimeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return time.Millisecond
	}
	if remaining < defaultTimeout {
		return remaining
	}
	return defaultTimeout
}
