package bacnet

import (
	"context"
	"fmt"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/encoding"
)

// ReadProperty reads a single property from a single object in the given device.
func (c *client) ReadProperty(device btypes.Device, rp btypes.PropertyData) (btypes.PropertyData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	id, err := c.tsm.ID(ctx)
	if err != nil {
		return btypes.PropertyData{}, fmt.Errorf("unable to get an transaction id: %v", err)
	}
	defer c.tsm.Put(id)
	enc := encoding.NewEncoder()
	device.Addr.SetLength()
	npdu := &btypes.NPDU{
		Version:               btypes.ProtocolVersion,
		Destination:           &device.Addr,
		Source:                c.dataLink.GetMyAddress(),
		IsNetworkLayerMessage: false,
		ExpectingReply:        true,
		Priority:              btypes.Normal,
		HopCount:              btypes.DefaultHopCount,
	}
	enc.NPDU(npdu)
	err = enc.ReadProperty(uint8(id), rp)
	if enc.Error() != nil || err != nil {
		return btypes.PropertyData{}, err
	}
	var lastErr error
	for range retryCount {
		var b []byte
		var out btypes.PropertyData
		_, err = c.Send(device.Addr, npdu, enc.Bytes(), nil)
		if err != nil {
			lastErr = fmt.Errorf("send read property request: %w", err)
			c.log.WithError(lastErr).Debug("read property send failed")
			continue
		}

		var raw interface{}
		raw, err = c.tsm.Receive(id, time.Duration(5)*time.Second)
		if err != nil {
			lastErr = fmt.Errorf("receive read property response: %w", err)
			continue
		}
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
			lastErr = fmt.Errorf("decode apdu: %w", err)
			continue
		}
		if apdu.Error.Class != 0 || apdu.Error.Code != 0 {
			lastErr = fmt.Errorf("received error, class: %d, code: %d", apdu.Error.Class, apdu.Error.Code)
			continue
		}
		if err = dec.ReadProperty(&out); err != nil {
			lastErr = fmt.Errorf("decode read property response: %w", err)
			continue
		}
		if err = dec.Error(); err != nil {
			lastErr = fmt.Errorf("decode read property response: %w", err)
			continue
		}
		return out, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("read property failed after %d retries", retryCount)
	}
	return btypes.PropertyData{}, lastErr
}
