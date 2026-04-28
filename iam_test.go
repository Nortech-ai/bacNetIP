package bacnet

import (
	"fmt"
	"go/build"
	"os"
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	pprint "github.com/Nortech-ai/bacNetIP/helpers/print"
	"github.com/stretchr/testify/assert"
)

var iface = "enp0s31f6"

func TestWhoIsRouterToNetwork(t *testing.T) {
	t.Skip("Skipping test")

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}
	fmt.Println(gopath)

	cb := &ClientBuilder{
		Interface: iface,
	}
	c, _ := NewClient(cb)
	defer c.Close()
	go c.ClientRun()

	//resp := c.WhatIsNetworkNumber()

	resp := c.WhoIsRouterToNetwork()
	fmt.Println("WhoIsRouterToNetwork")
	pprint.Print(resp)

}

func TestIAm(t *testing.T) {
	link := datalink.NewMockDataLink()
	cb := &ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	}

	c, err := NewClient(cb)
	assert.NoError(t, err)
	defer c.Close()

	device := btypes.Device{
		ID: btypes.ObjectID{
			Type:     btypes.DeviceType,
			Instance: 160101,
		},
		DeviceID:      160101,
		NetworkNumber: 1601,
		MacMSTP:       1,
		MaxApdu:       480,
		Segmentation:  0,
		Vendor:        0x18,
		Addr: btypes.Address{
			Net: 1601,
			Adr: []uint8{1},
			Len: 1,
		},
	}

	link.Received = []datalink.ReceivedMessage{
		{
			Data: []byte{0x81, 0xb, 0x0, 0x18, 0x1, 0x8, 0x6, 0x41, 0x1, 0x1, 0x10, 0x0, 0xc4, 0x2, 0x2, 0x71, 0x65, 0x22, 0x1, 0xe0, 0x91, 0x0, 0x21, 0x18},
			Src:  &device.Addr,
		},
	}

	go c.ClientRun()

	// Send a WhoIs
	devices, err := c.WhoIs(&WhoIsOpts{
		Low:             160000,
		High:            160102,
		GlobalBroadcast: false,
		NetworkNumber:   1601,
	})
	assert.NoError(t, err)

	assert.Equal(t, len(devices), 1)
	assert.Equal(t, devices[0], device)
}
