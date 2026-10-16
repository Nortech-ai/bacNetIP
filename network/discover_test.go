package network

import (
	"fmt"
	"net"
	"testing"

	"github.com/Nortech-ai/bacNetIP"
	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	pprint "github.com/Nortech-ai/bacNetIP/helpers/print"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscover(t *testing.T) {

	localDevice, err := New(&Network{Interface: iface, Port: 47808})
	if err != nil {
		fmt.Println("ERR-client", err)
		return
	}
	defer localDevice.NetworkClose(false)
	go localDevice.NetworkRun()

	device, err := NewDevice(localDevice, &Device{Ip: deviceIP, DeviceID: deviceID})
	if err != nil {
		return
	}

	objects, err := device.DeviceObjects(12, true)
	if err != nil {
		return
	}
	pprint.PrintJOSN(objects)

}

func TestGetPointsList(t *testing.T) {

	localDevice, err := New(&Network{Interface: iface, Port: 47808})
	if err != nil {
		fmt.Println("ERR-client", err)
		return
	}
	defer localDevice.NetworkClose(false)
	go localDevice.NetworkRun()

	device, err := NewDevice(localDevice, &Device{Ip: deviceIP, DeviceID: deviceID})
	if err != nil {
		return
	}

	objects, err := device.GetDevicePoints(12)
	if err != nil {
		return
	}
	pprint.PrintJOSN(objects)

}

type discoverClient struct {
	devices []btypes.Device
}

func (d *discoverClient) Close() error           { return nil }
func (d *discoverClient) ClientClose(bool) error { return nil }
func (d *discoverClient) ClientRun()             {}
func (d *discoverClient) WhoIs(*bacnet.WhoIsOpts) ([]btypes.Device, error) {
	return d.devices, nil
}
func (d *discoverClient) WhatIsNetworkNumber() []*btypes.Address { return nil }
func (d *discoverClient) IAm(btypes.Address, btypes.IAm) error   { return nil }
func (d *discoverClient) WhoIsRouterToNetwork() *[]btypes.Address {
	return nil
}
func (d *discoverClient) Objects(dev btypes.Device) (btypes.Device, error) {
	return dev, nil
}
func (d *discoverClient) ReadProperty(btypes.Device, btypes.PropertyData) (btypes.PropertyData, error) {
	return btypes.PropertyData{}, fmt.Errorf("no device")
}
func (d *discoverClient) ReadMultiProperty(dev btypes.Device, rp btypes.MultiplePropertyData) (btypes.MultiplePropertyData, error) {
	return rp, fmt.Errorf("no device")
}
func (d *discoverClient) WriteProperty(btypes.Device, btypes.PropertyData) error { return nil }
func (d *discoverClient) WriteMultiProperty(btypes.Device, btypes.MultiplePropertyData) error {
	return nil
}

func iamDevice(ip string, instance btypes.ObjectInstance) btypes.Device {
	addr := datalink.IPPortToAddress(net.ParseIP(ip).To4(), 0xBAC0)
	return btypes.Device{
		Ip:       ip,
		Port:     0xBAC0,
		DeviceID: int(instance),
		ID: btypes.ObjectID{
			Type:     btypes.DeviceType,
			Instance: instance,
		},
		Addr:    *addr,
		MaxApdu: 480,
	}
}

func TestDeviceDiscoverKeepsEachIAm(t *testing.T) {
	first := iamDevice("192.168.1.10", 10)
	second := iamDevice("192.168.1.20", 20)
	client := &discoverClient{devices: []btypes.Device{first, second}}
	receiver := &Device{
		Ip:       "10.0.0.1",
		DeviceID: 1,
		network:  client,
	}

	found, err := receiver.DeviceDiscover(nil)
	require.NoError(t, err)
	require.Len(t, found, 2)
	require.NotSame(t, found[0], found[1])
	require.NotSame(t, found[0], receiver)
	require.NotSame(t, found[1], receiver)
	assert.Equal(t, first.Addr.Mac, found[0].dev.Addr.Mac)
	assert.Equal(t, second.Addr.Mac, found[1].dev.Addr.Mac)
	assert.NotEqual(t, found[0].dev.Addr.Mac, found[1].dev.Addr.Mac)
	assert.Equal(t, "10.0.0.1", receiver.Ip)
	assert.Equal(t, 1, receiver.DeviceID)
}
