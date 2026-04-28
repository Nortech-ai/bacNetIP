package bacnet

import (
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/stretchr/testify/assert"
)

func TestReadMultiProperty(t *testing.T) {
	link := datalink.NewMockDataLink()
	cb := &ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	}

	c, err := NewClient(cb)
	assert.NoError(t, err)
	defer c.Close()

	go c.ClientRun()

	device := btypes.Device{
		ID: btypes.ObjectID{
			Type:     btypes.DeviceType,
			Instance: 160101,
		},
		Addr: btypes.Address{
			Net: 1601,
			Adr: []uint8{1},
			Len: 1,
		},
		MaxApdu: 480,
	}

	rp := btypes.MultiplePropertyData{
		Objects: []btypes.Object{
			{
				ID: btypes.ObjectID{
					Type:     btypes.BinaryValue,
					Instance: 175,
				},
				Properties: []btypes.Property{
					{
						Type:       btypes.PropUnits,
						ArrayIndex: btypes.ArrayAll,
					},
				},
			},
		},
	}

	link.Received = []datalink.ReceivedMessage{
		{
			Data: []byte{0x81, 0xa, 0x0, 0x4a, 0x1, 0x0, 0x30, 0x1, 0xe, 0xc, 0x1, 0x40, 0xd, 0xaf, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x0, 0x4f, 0x1f, 0xc, 0x1, 0x40, 0x7, 0xee, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x1, 0x4f, 0x1f, 0xc, 0x1, 0x40, 0x13, 0xa6, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x0, 0x4f, 0x1f, 0xc, 0x1, 0x7d, 0xc, 0xeb, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x0, 0x4f, 0x1f, 0xc, 0x1, 0x40, 0x27, 0x16, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x1, 0x4f, 0x1f},
			Src:  &device.Addr,
		},
	}

	props, err := c.ReadMultiProperty(device, rp)
	assert.NoError(t, err)
	assert.Equal(t, len(props.Objects), 5)
	assert.Equal(t, len(props.Objects[4].Properties), 1)
	assert.Equal(t, props.Objects[0].ID.Instance, btypes.ObjectInstance(3503))
	assert.Equal(t, props.Objects[0].Properties[0].Type, btypes.PropPresentValue)
	assert.Equal(t, props.Objects[0].Properties[0].Data, uint32(0))
}
