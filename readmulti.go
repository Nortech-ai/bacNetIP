package bacnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"

	"github.com/Nortech-ai/bacNetIP/encoding"
)

// ErrReplyMismatch is returned when a ReadPropertyMultiple complex-ack does
// not list the objects and property ids that were requested.
var ErrReplyMismatch = errors.New("bacnet reply does not match the request")

const maxReattempt = 2

// ReadMultiProperty uses the given device and read property request to read
// from a device. Along with being able to read multiple properties from a
// device, it can also read these properties from multiple objects. This is a
// good feature to read all present values of every object in the device. This
// is a batch operation compared to a ReadProperty and should be used in place
// when reading more than two objects/properties.
func (c *client) ReadMultiProperty(device btypes.Device, rp btypes.MultiplePropertyData) (btypes.MultiplePropertyData, error) {
	var out btypes.MultiplePropertyData

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	device.Addr.SetLength()
	id, err := c.tsm.ID(ctx, &device.Addr, btypes.ServiceConfirmedReadPropMultiple)
	if err != nil {
		return out, fmt.Errorf("unable to get transaction id: %v", err)
	}
	defer c.tsm.Put(id)
	err = device.CheckADPU()
	if err != nil {
		return btypes.MultiplePropertyData{}, err
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
		return out, fmt.Errorf("encoding read multiple property failed: %v", err)
	}

	pack := enc.Bytes()
	if device.MaxApdu < uint32(len(pack)) {
		return out, fmt.Errorf("read multiple property is too large (max: %d given: %d)", device.MaxApdu, len(pack))
	}
	// the value filled doesn't matter. it just needs to be non nil
	err = fmt.Errorf("go")

	for count := 0; err != nil && count < maxReattempt; count++ {
		out, err = c.sendReadMultipleProperty(id, device, rp, npdu, pack)
		if err == nil {
			return out, nil
		}
		// The ack already completed the transaction. Another try would time out,
		// and the %v wrap below would hide the sentinel.
		if errors.Is(err, ErrReplyMismatch) {
			return btypes.MultiplePropertyData{}, err
		}
	}
	return out, fmt.Errorf("failed %d tries: %v", maxReattempt, err)
}

func (c *client) sendReadMultipleProperty(id int, dev btypes.Device, rp btypes.MultiplePropertyData, npdu *btypes.NPDU, request []byte) (btypes.MultiplePropertyData, error) {
	var out btypes.MultiplePropertyData
	_, err := c.Send(dev.Addr, npdu, request, nil)
	if err != nil {
		return out, err
	}

	raw, err := c.tsm.Receive(id, time.Duration(5)*time.Second)
	if err != nil {
		return out, fmt.Errorf("unable to receive id %d: %v", id, err)
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
		return out, err
	}
	if apdu.Error.Class != 0 || apdu.Error.Code != 0 {
		err = fmt.Errorf("received error, class: %d, code: %d", apdu.Error.Class, apdu.Error.Code)
		return out, err
	}
	err = dec.ReadMultiplePropertyAck(&out)
	if err != nil {
		c.log.Debugf("WEIRD PACKET: %v: %v", err, b)
		return out, err
	}
	if err = matchReadMultipleReply(rp, out); err != nil {
		c.log.Debug("bacnet reply does not match the request")
		return btypes.MultiplePropertyData{}, err
	}
	return out, nil
}

// matchReadMultipleReply checks that the complex-ack lists the requested objects
// in order and only property ids that were asked for. Property order and values
// are ignored. A non-empty subset of the requested ids is accepted, including a
// property whose Data is an error.
func matchReadMultipleReply(requested, reply btypes.MultiplePropertyData) error {
	if len(reply.Objects) != len(requested.Objects) {
		return fmt.Errorf("%w: object count %d, requested %d", ErrReplyMismatch, len(reply.Objects), len(requested.Objects))
	}
	for i := range requested.Objects {
		reqObj := requested.Objects[i]
		repObj := reply.Objects[i]

		if !matchObjectID(reqObj.ID, repObj.ID) {
			return fmt.Errorf("%w: object %d id mismatch (requested %s, reply %s)", ErrReplyMismatch, i, reqObj.ID.String(), repObj.ID.String())
		}

		var hasWildcardProp, hasOptionalProp bool
		allowed := make(map[btypes.PropertyType]struct{}, len(reqObj.Properties))
		for _, prop := range reqObj.Properties {
			switch prop.Type {
			// PropAllProperties (PROP_ALL = 8) and PROP_REQUIRED (105)
			case btypes.PropAllProperties, btypes.PROP_REQUIRED:
				hasWildcardProp = true
			case btypes.PROP_OPTIONAL:
				hasWildcardProp = true
				hasOptionalProp = true
			default:
				allowed[prop.Type] = struct{}{}
			}
		}

		// Clause 15.7.2 & BTL IC 135-2010-6: An empty property list is valid ONLY when
		// PROP_OPTIONAL was requested and the object has no optional properties.
		if len(repObj.Properties) == 0 && !hasOptionalProp {
			return fmt.Errorf("%w: object %d has no properties", ErrReplyMismatch, i)
		}

		if hasWildcardProp {
			continue
		}

		for _, prop := range repObj.Properties {
			if _, ok := allowed[prop.Type]; !ok {
				return fmt.Errorf("%w: object %d property %d was not requested", ErrReplyMismatch, i, prop.Type)
			}
		}
	}
	return nil
}

func matchObjectID(requested, reply btypes.ObjectID) bool {
	if requested == reply {
		return true
	}
	// BACnet Clause 15.7.2 (Addendum 135-2001a / 135-2016br-7): Device instance
	// 4194303 (btypes.MaxInstance) is the indefinite/wildcard identifier matching
	// the responding device's local Device object.
	if requested.Type == btypes.DeviceType && requested.Instance == btypes.MaxInstance {
		return reply.Type == btypes.DeviceType && reply.Instance < btypes.MaxInstance
	}
	return false
}
