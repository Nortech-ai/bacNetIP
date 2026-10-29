package bacnet

import (
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/btypes/bacerr"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/Nortech-ai/bacNetIP/encoding"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	link.Inject(datalink.ReceivedMessage{
		Data: []byte{0x81, 0xa, 0x0, 0x4a, 0x1, 0x0, 0x30, 0x1, 0xe, 0xc, 0x1, 0x40, 0xd, 0xaf, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x0, 0x4f, 0x1f, 0xc, 0x1, 0x40, 0x7, 0xee, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x1, 0x4f, 0x1f, 0xc, 0x1, 0x40, 0x13, 0xa6, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x0, 0x4f, 0x1f, 0xc, 0x1, 0x7d, 0xc, 0xeb, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x0, 0x4f, 0x1f, 0xc, 0x1, 0x40, 0x27, 0x16, 0x1e, 0x29, 0x55, 0x4e, 0x91, 0x1, 0x4f, 0x1f},
		Src:  &device.Addr,
	})

	props, err := c.ReadMultiProperty(device, rp)
	require.ErrorIs(t, err, ErrReplyMismatch)
	require.Empty(t, props.Objects)
}

func TestReadMultiPropertyReplyMatch(t *testing.T) {
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
	requestedIDs := objectIDs(btypes.BinaryInput, 80, 85, 46, 26, 115)
	request := objectsWithProperty(requestedIDs, btypes.PropPresentValue, nil)
	matching := []interface{}{float32(1.5), uint32(1), float32(2), uint32(0), float32(4)}

	tests := []struct {
		name     string
		request  []btypes.Object
		reply    []btypes.Object
		errorAt  int
		mismatch bool
	}{
		{
			name: "foreign objects",
			reply: objectsWithProperty(
				objectIDs(btypes.AnalogInput, 29, 19, 25, 27, 30),
				btypes.PropUnits,
				[]interface{}{"FD71905OP", uint32(1), uint32(1), uint32(1), uint32(1)},
			),
			errorAt:  -1,
			mismatch: true,
		},
		{
			name: "wrong property id",
			reply: objectsWithProperty(
				requestedIDs,
				btypes.PropUnits,
				[]interface{}{uint32(95), uint32(1), uint32(1), uint32(1), uint32(1)},
			),
			errorAt:  -1,
			mismatch: true,
		},
		{
			name:     "short reply",
			reply:    objectsWithProperty(requestedIDs[:3], btypes.PropPresentValue, []interface{}{uint32(0), uint32(1), uint32(0)}),
			errorAt:  -1,
			mismatch: true,
		},
		{
			name: "extra object",
			reply: append(
				objectsWithProperty(requestedIDs, btypes.PropPresentValue, matching),
				objectsWithProperty(objectIDs(btypes.AnalogInput, 1), btypes.PropPresentValue, []interface{}{float32(1)})...,
			),
			errorAt:  -1,
			mismatch: true,
		},
		{
			name:     "matching values",
			reply:    objectsWithProperty(requestedIDs, btypes.PropPresentValue, matching),
			errorAt:  -1,
			mismatch: false,
		},
		{
			name:     "property access error",
			reply:    objectsWithProperty(requestedIDs, btypes.PropPresentValue, matching),
			errorAt:  2,
			mismatch: false,
		},
		{
			name: "wildcard device instance accepted",
			request: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.DeviceType, Instance: btypes.MaxInstance},
					Properties: []btypes.Property{{Type: btypes.PropObjectName, ArrayIndex: btypes.ArrayAll}},
				},
			},
			reply: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.DeviceType, Instance: 160101},
					Properties: []btypes.Property{{Type: btypes.PropObjectName, ArrayIndex: btypes.ArrayAll, Data: "Device-160101"}},
				},
			},
			errorAt:  -1,
			mismatch: false,
		},
		{
			name: "wildcard instance rejected for non-device type",
			request: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: btypes.MaxInstance},
					Properties: []btypes.Property{{Type: btypes.PropPresentValue, ArrayIndex: btypes.ArrayAll}},
				},
			},
			reply: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{{Type: btypes.PropPresentValue, ArrayIndex: btypes.ArrayAll, Data: float32(1)}},
				},
			},
			errorAt:  -1,
			mismatch: true,
		},
		{
			name: "prop all accepts expanded properties",
			request: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{{Type: btypes.PropAllProperties, ArrayIndex: btypes.ArrayAll}},
				},
			},
			reply: []btypes.Object{
				{
					ID: btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{
						{Type: btypes.PropObjectName, ArrayIndex: btypes.ArrayAll, Data: "AI-1"},
						{Type: btypes.PropPresentValue, ArrayIndex: btypes.ArrayAll, Data: float32(21.5)},
					},
				},
			},
			errorAt:  -1,
			mismatch: false,
		},
		{
			name: "prop optional accepts empty properties list",
			request: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{{Type: btypes.PROP_OPTIONAL, ArrayIndex: btypes.ArrayAll}},
				},
			},
			reply: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{},
				},
			},
			errorAt:  -1,
			mismatch: false,
		},
		{
			name: "empty properties list rejected without prop optional",
			request: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{{Type: btypes.PropPresentValue, ArrayIndex: btypes.ArrayAll}},
				},
			},
			reply: []btypes.Object{
				{
					ID:         btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1},
					Properties: []btypes.Property{},
				},
			},
			errorAt:  -1,
			mismatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := datalink.NewMockDataLink()
			raw, err := NewClient(&ClientBuilder{
				DataLink: link,
				Ip:       "192.168.1.100",
			})
			require.NoError(t, err)
			c := raw.(*client)
			t.Cleanup(func() { _ = c.Close() })
			go c.ClientRun()

			var hook *logtest.Hook
			if tt.mismatch {
				c.log.SetLevel(log.DebugLevel)
				hook = logtest.NewLocal(c.log)
			}

			req := tt.request
			if req == nil {
				req = request
			}

			link.Inject(datalink.ReceivedMessage{
				Data: readMultipleAckPacket(t, &device.Addr, tt.reply, tt.errorAt),
				Src:  &device.Addr,
			})

			got, err := c.ReadMultiProperty(device, btypes.MultiplePropertyData{Objects: req})
			if tt.mismatch {
				require.ErrorIs(t, err, ErrReplyMismatch)
				require.Empty(t, got.Objects)
				assertMismatchLogged(t, hook)
				return
			}

			require.NoError(t, err)
			require.Len(t, got.Objects, len(tt.reply))
			for i := range tt.reply {
				assert.Equal(t, tt.reply[i].ID, got.Objects[i].ID)
				require.Len(t, got.Objects[i].Properties, len(tt.reply[i].Properties))
				for j, prop := range tt.reply[i].Properties {
					gotProp := got.Objects[i].Properties[j]
					assert.Equal(t, prop.Type, gotProp.Type)
					if i == tt.errorAt {
						_, ok := gotProp.Data.(error)
						assert.True(t, ok)
						continue
					}
					assert.Equal(t, prop.Data, gotProp.Data)
				}
			}
		})
	}
}

func assertMismatchLogged(t *testing.T, hook *logtest.Hook) {
	t.Helper()
	var sawDebug bool
	for _, entry := range hook.AllEntries() {
		if entry.Message == "bacnet reply does not match the request" {
			require.Equal(t, log.DebugLevel, entry.Level)
			sawDebug = true
		}
		require.NotEqual(t, log.ErrorLevel, entry.Level)
	}
	require.True(t, sawDebug)
}

func objectIDs(typ btypes.ObjectType, instances ...btypes.ObjectInstance) []btypes.ObjectID {
	ids := make([]btypes.ObjectID, len(instances))
	for i, inst := range instances {
		ids[i] = btypes.ObjectID{Type: typ, Instance: inst}
	}
	return ids
}

func objectsWithProperty(ids []btypes.ObjectID, prop btypes.PropertyType, values []interface{}) []btypes.Object {
	objects := make([]btypes.Object, len(ids))
	for i, id := range ids {
		var data interface{}
		if i < len(values) {
			data = values[i]
		}
		objects[i] = btypes.Object{
			ID: id,
			Properties: []btypes.Property{{
				Type:       prop,
				ArrayIndex: btypes.ArrayAll,
				Data:       data,
			}},
		}
	}
	return objects
}

func readMultipleAckPacket(t *testing.T, src *btypes.Address, objects []btypes.Object, errorAt int) []byte {
	t.Helper()
	var apdu []byte
	if errorAt < 0 {
		enc := encoding.NewEncoder()
		require.NoError(t, enc.ReadMultiplePropertyAck(1, btypes.MultiplePropertyData{Objects: objects}))
		require.NoError(t, enc.Error())
		apdu = enc.Bytes()
	} else {
		header := encoding.NewEncoder()
		require.NoError(t, header.APDU(btypes.APDU{
			DataType: btypes.ComplexAck,
			Service:  btypes.ServiceConfirmedReadPropMultiple,
			InvokeId: 1,
		}))
		require.NoError(t, header.Error())
		apdu = append([]byte{}, header.Bytes()...)
		for i, obj := range objects {
			if i == errorAt {
				require.NotEmpty(t, obj.Properties)
				apdu = append(apdu, propertyAccessErrorObject(t, obj.ID, obj.Properties[0].Type, uint32(bacerr.PropertyError), uint32(bacerr.UnknownProperty))...)
				continue
			}
			one := encoding.NewEncoder()
			require.NoError(t, one.ReadMultiplePropertyAck(1, btypes.MultiplePropertyData{Objects: []btypes.Object{obj}}))
			require.NoError(t, one.Error())
			raw := one.Bytes()
			require.GreaterOrEqual(t, len(raw), 3)
			apdu = append(apdu, raw[3:]...)
		}
	}
	return bvlcWithNPDU(t, btypes.BacFuncUnicast, src, nil, apdu)
}

func propertyAccessErrorObject(t *testing.T, id btypes.ObjectID, prop btypes.PropertyType, class, code uint32) []byte {
	t.Helper()
	require.Less(t, uint32(prop), uint32(256))
	require.Less(t, class, uint32(256))
	require.Less(t, code, uint32(256))
	v := (uint32(id.Type) << 22) | uint32(id.Instance)
	return []byte{
		0x0C,
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		0x1E,
		0x29, byte(prop),
		0x5E,
		0x21, byte(class),
		0x21, byte(code),
		0x5F,
		0x1F,
	}
}
