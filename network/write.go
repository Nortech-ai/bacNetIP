package network

import (
	"fmt"

	bacnet "github.com/Nortech-ai/bacNetIP"
	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/btypes/null"
)

type Write struct {
	ObjectID      btypes.ObjectInstance
	ObjectType    btypes.ObjectType
	Prop          btypes.PropertyType
	WriteValue    interface{}
	WriteNull     bool
	WritePriority uint8
}

func (device *Device) Write(write *Write) error {
	var err error
	writeValue := write.WriteValue

	rp := btypes.PropertyData{
		Object: btypes.Object{
			ID: btypes.ObjectID{
				Type:     write.ObjectType,
				Instance: btypes.ObjectInstance(write.ObjectID),
			},
			Properties: []btypes.Property{
				{
					Type:       write.Prop,
					ArrayIndex: bacnet.ArrayAll,
					Priority:   btypes.PropertyPriority(write.WritePriority),
				},
			},
		},
	}

	if write.WriteNull {
		writeValue = null.Null{}
	} else {
		switch writeValue.(type) {
		case uint32, float32, float64, string:
		default:
			return fmt.Errorf("unable to handle a type %T", writeValue)
		}
	}

	rp.Object.Properties[0].Data = writeValue
	// log.Printf("Writting: %v", writeValue)
	err = device.network.WriteProperty(device.dev, rp)
	if err != nil {
		return err
	}
	return nil
}
