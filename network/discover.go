package network

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/Nortech-ai/bacNetIP"
	"github.com/Nortech-ai/bacNetIP/btypes"
)

type DevicePoints struct {
	Name                      string            `json:"name"`
	MaxApdu                   uint32            `json:"max_apdu"`
	VendorName                string            `json:"vendor_name"`
	Segmentation              uint32            `json:"segmentation"`
	ProtocolServicesSupported *btypes.BitString `json:"protocol_services_supported"`
}

// GetDevicePoints build a device points list
// first read device and see what it supports and get the name and so on
// try and get the object list if it's an error then loop through the arrayIndex to build the object list
// with the object list do a point's discovery, get the name, units and so on
func (device *Device) GetDevicePoints(deviceID btypes.ObjectInstance) (resp []*PointDetails, err error) {
	resp = []*PointDetails{}
	list, err := device.DeviceObjects(deviceID, true)
	if err != nil {
		return nil, err
	}
	pntDetails := &Point{}
	for _, obj := range list {

		if obj.Type != 8 {
			pntDetails = &Point{
				ObjectID:   obj.Instance,
				ObjectType: obj.Type,
			}
			details, _ := device.PointDetails(pntDetails)
			resp = append(resp, details)
		}
	}
	return resp, nil

}

type DeviceDetails struct {
	Name                      string            `json:"name"`
	MaxApdu                   uint32            `json:"max_apdu"`
	VendorName                string            `json:"vendor_name"`
	Segmentation              uint32            `json:"segmentation"`
	ProtocolServicesSupported *btypes.BitString `json:"protocol_services_supported"`
}

// GetDeviceDetails get the device name, max adpu and so on
// first read device and see what it supports and get the name and so on
// try and get the object list if it's an error then loop through the arrayIndex to build the object list
func (device *Device) GetDeviceDetails(deviceID btypes.ObjectInstance) (resp *DeviceDetails, err error) {
	resp = &DeviceDetails{}
	obj := &Object{
		ObjectID:   deviceID,
		ObjectType: btypes.TypeDeviceType,
		Prop:       btypes.PropObjectName,
		ArrayIndex: bacnet.ArrayAll,
	}
	props := []btypes.PropertyType{btypes.PropObjectName, btypes.PropMaxAPDU, btypes.PropVendorName, btypes.PropSegmentationSupported}
	for _, prop := range props {
		obj.Prop = prop
		read, err := device.Read(obj)
		if err != nil {
			log.Errorln("bacnet-master-GetDeviceDetails()", err.Error())
		}
		switch prop {
		case btypes.PropObjectName:
			resp.Name = device.toStr(read)
		case btypes.PropMaxAPDU:
			resp.MaxApdu = device.toUint32(read)
		case btypes.PropVendorName:
			resp.VendorName = device.toStr(read)
		case btypes.PropSegmentationSupported:
			resp.Segmentation = device.toUint32(read)
		case btypes.ProtocolServicesSupported:
			resp.ProtocolServicesSupported = device.ToBitString(read)
		}
	}
	log.Infoln("bacnet-device name:", resp.Name)
	log.Infoln("bacnet-device vendor-name:", resp.VendorName)
	return resp, nil
}

func (device *Device) DeviceDiscover(options *bacnet.WhoIsOpts) ([]*Device, error) {
	whois, err := device.Whois(options)
	var devices []*Device
	if err != nil {
		return devices, err
	}
	for _, dev := range whois {
		found, nerr := deviceFromIAm(device.network, dev)
		if nerr != nil {
			return devices, nerr
		}
		details, derr := found.GetDeviceDetails(dev.ID.Instance)
		if derr != nil {
			fmt.Println("discover err", derr)
		}
		found.DeviceName = details.Name
		found.VendorName = details.VendorName
		devices = append(devices, found)
	}
	count := 1
	for _, d := range devices {
		fmt.Println("devices found", count)
		count++
		fmt.Println(d.VendorName, d.DeviceName, d.Ip)
	}
	return devices, err
}

// deviceFromIAm copies one I-Am into its own Device and sets dev.Addr from that I-Am.
// The receiver used for discovery is left unchanged.
func deviceFromIAm(client bacnet.Client, dev btypes.Device) (*Device, error) {
	mac := 0
	if len(dev.Addr.Adr) > 0 {
		mac = int(dev.Addr.Adr[0])
	}
	host, err := dev.Addr.UDPAddr()
	if err != nil {
		return nil, fmt.Errorf("i-am address: %w", err)
	}
	found := &Device{
		Ip:            host.IP.String(),
		Port:          host.Port,
		DeviceID:      int(dev.ID.Instance),
		NetworkNumber: int(dev.Addr.Net),
		MacMSTP:       mac,
		MaxApdu:       dev.MaxApdu,
		Segmentation:  uint32(dev.Segmentation),
		network:       client,
	}
	// Store with a nil cache skips persistence. NewDevice fills dev.Addr.
	created, err := NewDevice(&Network{bacnet: client, Store: &Store{}}, found)
	if err != nil {
		return nil, err
	}
	return created, nil
}
