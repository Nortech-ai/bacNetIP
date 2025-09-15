package bacnet

import (
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/stretchr/testify/assert"
)

func TestWriteProperty(t *testing.T) {
	link := datalink.NewMockDataLink()
	link.MyAddress = nil
	cb := &ClientBuilder{
		DataLink: link,
		Ip:       "192.168.0.50",
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
			Net: 3,
			Adr: []uint8{108},
			Len: 1,
		},
		NetworkNumber: 3,
		MaxApdu:       480,
		Segmentation:  btypes.Enumerated(2),
	}

	// Create a comprehensive Write Property request
	wp := btypes.PropertyData{
		Object: btypes.Object{
			ID: btypes.ObjectID{
				Type:     btypes.AnalogOutput, // Object type
				Instance: 101,                 // Object instance
			},
			Properties: []btypes.Property{
				{
					Type:       btypes.PropPresentValue,                // Property to write
					ArrayIndex: btypes.ArrayAll,                        // Array index (0xFFFFFFFF for all)
					Priority:   btypes.PropertyPriorityManualOperator3, // Priority level (16 = lowest)
					Data:       float32(50.0),                          // Value to write
				},
			},
		},
		ErrorClass: 0, // No error initially
		ErrorCode:  0, // No error initially
	}

	// Expected BACnet Write Property message (matching real network dump)
	expectedSent := []datalink.SentMessage{
		{
			Data: []byte{
				0x81, 0x0a, 0x00, 0x1f, //Virtual Link Control Header
				0x01, 0x24, 0x00, 0x03, 0x01, 0x6c, 0xff, //Network Layer Header
				0x00, 0x05, 0x01, 0x0f, 0x0c, 0x00, 0x40, 0x00, //APDU
				0x65, 0x19, 0x55, 0x3e, 0x44, 0x42, 0x48, 0x00, //APDU Data
				0x00, 0x3f, 0x49, 0x0a, //APDU Data
			},
			Dest: &device.Addr,
		},
	}

	// Mock the real response from your network dump
	link.Received = []datalink.ReceivedMessage{
		{
			Data: []byte{0x81, 0x0a, 0x00, 0x0d,
				0x1, 0x08, 0x00, 0x03, 0x01, 0x6c,
				0x20, 0x01, 0x0f,
			},
			Src: &device.Addr,
		},
	}

	err = c.WriteProperty(device, wp)
	assert.NoError(t, err)

	// Verify the message was sent correctly
	assert.Equal(t, 1, len(link.Sent))
	assert.Equal(t, expectedSent[0].Dest, link.Sent[0].Dest)
}
