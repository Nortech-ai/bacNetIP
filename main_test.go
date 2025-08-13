package bacnet

import (
	"encoding/json"
	"fmt"
	"log"
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/Nortech-ai/bacNetIP/encoding"
)

const interfaceName = "eth0"
const testServer = 260001

// TestMain are general test
func TestUdpDataLink(t *testing.T) {
	c, _ := NewClient(&ClientBuilder{Interface: interfaceName})
	c.Close()

	_, err := datalink.NewUDPDataLink("pizzainterfacenotreal", 0)
	if err == nil {
		t.Fatal("Successfully passed a false interface.")
	}
}

func TestMac(t *testing.T) {
	var mac []byte
	json.Unmarshal([]byte("\"ChQAzLrA\""), &mac)
	l := len(mac)
	p := uint16(mac[l-1])<<8 | uint16(mac[l-1])
	log.Printf("%d", p)
}

func TestServices(t *testing.T) {
	c, _ := NewClient(&ClientBuilder{Interface: interfaceName})
	defer c.Close()

	t.Run("Read Property", func(t *testing.T) {
		testReadPropertyService(c, t)
	})

	t.Run("Who Is", func(t *testing.T) {
		testWhoIs(c, t)
	})

	t.Run("WriteProperty", func(t *testing.T) {
		testWritePropertyService(c, t)
	})

}

func TestMain(t *testing.T) {
	c, err := NewClient(&ClientBuilder{
		Interface: "en0",
		Ip:        "192.168.6.212",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	go c.ClientRun()

	wh := &WhoIsOpts{
		GlobalBroadcast: true,
		NetworkNumber:   0,
	}
	wh.Low = testServer - 1
	wh.High = testServer + 1
	devs, err := c.WhoIs(wh)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println((devs))
	ip := "192.168.6.212"
	port := 47808

	mac := make([]byte, 6)
	fmt.Sscanf(ip, "%d.%d.%d.%d", &mac[0], &mac[1], &mac[2], &mac[3])
	mac[4] = byte(port >> 8)
	mac[5] = byte(port & 0x00FF)

	bacnetDev := btypes.Device{
		DeviceID: 1234,
		Ip:       ip,
		Port:     port,
		MaxApdu:  1476,
		Addr: btypes.Address{
			MacLen: 6,
			Mac:    mac,
		},
	}
	// bacnetDev, err := c.Objects(dev)
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }

	prop, err := c.ReadProperty(
		bacnetDev,
		btypes.PropertyData{
			Object: btypes.Object{
				ID: btypes.ObjectID{
					Type:     btypes.AnalogOutput,
					Instance: 1,
				},
				Properties: []btypes.Property{{
					Type:       btypes.PropPresentValue,
					ArrayIndex: encoding.ArrayAll,
				}},
			},
		})
	if err != nil {
		fmt.Println("rp1", err)
		return
	}

	value := fmt.Sprintf("%v", prop.Object.Properties[0].Data)
	fmt.Println(value)

	wp := btypes.PropertyData{
		Object: btypes.Object{
			ID: btypes.ObjectID{
				Type:     btypes.AnalogOutput,
				Instance: 1,
			},
			Properties: []btypes.Property{
				{
					Type:       btypes.PropPresentValue, // Present value
					ArrayIndex: ArrayAll,
					Priority:   btypes.Normal,
					Data:       float32(1),
				},
			},
		},
	}
	err = c.WriteProperty(bacnetDev, wp)
	if err != nil {
		fmt.Println("wp", err)
		return
	}

	prop, err = c.ReadProperty(
		bacnetDev,
		btypes.PropertyData{
			Object: btypes.Object{
				ID: btypes.ObjectID{
					Type:     btypes.AnalogOutput,
					Instance: 1,
				},
				Properties: []btypes.Property{{
					Type:       btypes.PropPresentValue,
					ArrayIndex: encoding.ArrayAll,
				}},
			},
		})
	if err != nil {
		fmt.Println("rp2", err)
		return
	}

	value = fmt.Sprintf("%v", prop.Object.Properties[0].Data)
	fmt.Println(value)

	props, err := c.ReadMultiProperty(bacnetDev, btypes.MultiplePropertyData{Objects: []btypes.Object{
		{
			ID: btypes.ObjectID{
				Type:     btypes.AnalogInput,
				Instance: 0,
			},
			Properties: []btypes.Property{
				{
					Type:       btypes.PropAllProperties,
					ArrayIndex: encoding.ArrayAll,
				},
				{
					Type:       btypes.PropPresentValue,
					ArrayIndex: encoding.ArrayAll,
				},
			},
		},
	}})

	fmt.Println(props.Objects)
	if err != nil {
		fmt.Println("rmp", err)
		return
	}
}

func testReadPropertyService(c Client, t *testing.T) {
	wh := &WhoIsOpts{
		GlobalBroadcast: false,
		NetworkNumber:   0,
	}
	wh.Low = testServer
	wh.High = testServer
	dev, err := c.WhoIs(wh)
	read := btypes.PropertyData{
		Object: btypes.Object{
			ID: btypes.ObjectID{
				Type:     btypes.AnalogValue,
				Instance: 1,
			},
			Properties: []btypes.Property{
				{
					Type:       btypes.PropObjectName, // Present value
					ArrayIndex: ArrayAll,
				},
			},
		},
	}
	if len(dev) == 0 {
		t.Fatalf("Unable to find device id %d", testServer)
	}

	resp, err := c.ReadProperty(dev[0], read)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Response: %v", resp.Object.Properties[0].Data)
}

func testWhoIs(c Client, t *testing.T) {
	wh := &WhoIsOpts{
		GlobalBroadcast: true,
		NetworkNumber:   0,
	}
	wh.Low = testServer - 1
	wh.High = testServer + 1
	dev, err := c.WhoIs(wh)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev) == 0 {
		t.Fatalf("Unable to find device id %d", testServer)
	}
}

// This test will first cconver the name of an analogue sensor to a different
// value, read the property to make sure the name was changed, revert back, and
// ensure that the revert was successful
func testWritePropertyService(c Client, t *testing.T) {
	const targetName = "Hotdog"
	wh := &WhoIsOpts{
		GlobalBroadcast: false,
		NetworkNumber:   0,
	}
	wh.Low = testServer
	wh.High = testServer
	dev, err := c.WhoIs(wh)
	wp := btypes.PropertyData{
		Object: btypes.Object{
			ID: btypes.ObjectID{
				Type:     btypes.AnalogValue,
				Instance: 1,
			},
			Properties: []btypes.Property{
				{
					Type:       btypes.PropObjectName, // Present value
					ArrayIndex: ArrayAll,
					Priority:   btypes.Normal,
				},
			},
		},
	}

	if len(dev) == 0 {
		t.Fatalf("Unable to find device id %d", testServer)
	}
	resp, err := c.ReadProperty(dev[0], wp)
	if err != nil {
		t.Fatal(err)
	}
	// Store the original response since we plan to put it back in after
	org := resp.Object.Properties[0].Data
	t.Logf("original name is: %d", org)

	wp.Object.Properties[0].Data = targetName
	err = c.WriteProperty(dev[0], wp)
	if err != nil {
		t.Fatal(err)
	}

	resp, err = c.ReadProperty(dev[0], wp)
	if err != nil {
		t.Fatal(err)
	}

	d := resp.Object.Properties[0].Data
	s, ok := d.(string)
	if !ok {
		log.Fatalf("unexpected return type %T", d)
	}

	if s != targetName {
		log.Fatalf("write to name %s did not successed, name was %s", targetName, s)
	}

	// Revert Changes
	wp.Object.Properties[0].Data = org
	err = c.WriteProperty(dev[0], wp)
	if err != nil {
		t.Fatal(err)
	}

	resp, err = c.ReadProperty(dev[0], wp)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Object.Properties[0].Data != org {
		t.Fatalf("unable to revert name back to original value %v: name is %v", org, resp.Object.Properties[0].Data)
	}
}

func TestDeviceClient(t *testing.T) {
	c, _ := NewClient(&ClientBuilder{Interface: interfaceName})
	go c.ClientRun()
	wh := &WhoIsOpts{
		GlobalBroadcast: false,
		NetworkNumber:   0,
	}
	wh.Low = testServer - 1
	wh.High = testServer + 1
	devs, err := c.WhoIs(wh)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%+v\n", devs)
	//	c.Objects(devs[0])

	prop, err := c.ReadProperty(
		devs[0],
		btypes.PropertyData{
			Object: btypes.Object{
				ID: btypes.ObjectID{
					Type:     btypes.AnalogInput,
					Instance: 0,
				},
				Properties: []btypes.Property{{
					Type:       85,
					ArrayIndex: encoding.ArrayAll,
				}},
			},
			ErrorClass: 0,
			ErrorCode:  0,
		})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(prop.Object.Properties)

	props, err := c.ReadMultiProperty(devs[0], btypes.MultiplePropertyData{Objects: []btypes.Object{
		{
			ID: btypes.ObjectID{
				Type:     btypes.AnalogInput,
				Instance: 0,
			},
			Properties: []btypes.Property{
				{
					Type:       8,
					ArrayIndex: encoding.ArrayAll,
				},
				/*	{
					Type:       85,
					ArrayIndex: encoding.ArrayAll,
				},*/
			},
		},
	}})

	fmt.Println(props)
	if err != nil {
		fmt.Println(err)
		return
	}
}
