package bacnet

import (
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractObjectMetadata(t *testing.T) {
	devID := btypes.ObjectID{Type: btypes.DeviceType, Instance: 607}
	objID := btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1}

	tests := []struct {
		name    string
		reply   btypes.Object
		wantErr bool
	}{
		{
			name: "swapped property order",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
				{Type: btypes.PropObjectName, Data: "AI-1"},
			}},
		},
		{
			name: "extra properties ignored",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropDescription, Data: "desc"},
				{Type: btypes.PropObjectName, Data: "AI-1"},
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			}},
		},
		{
			name: "fewer than two properties",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropObjectName, Data: "AI-1"},
			}},
			wantErr: true,
		},
		{
			name: "wrong property ids",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropPresentValue, Data: float32(1)},
				{Type: btypes.PropUnits, Data: uint32(95)},
			}},
			wantErr: true,
		},
		{
			name: "wrong object id",
			reply: btypes.Object{
				ID: btypes.ObjectID{Type: btypes.BinaryInput, Instance: 80},
				Properties: []btypes.Property{
					{Type: btypes.PropObjectName, Data: "FD71905OP"},
					{Type: btypes.PropObjectType, Data: uint32(btypes.BinaryInput)},
				},
			},
			wantErr: true,
		},
		{
			name: "wrong object type",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropObjectName, Data: "AI-1"},
				{Type: btypes.PropObjectType, Data: uint32(btypes.BinaryInput)},
			}},
			wantErr: true,
		},
		{
			name: "object type as float32 rejected",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropObjectType, Data: float32(btypes.AnalogInput)},
				{Type: btypes.PropObjectName, Data: "AI-1"},
			}},
			wantErr: true,
		},
		{
			name: "wrong object name type",
			reply: btypes.Object{ID: objID, Properties: []btypes.Property{
				{Type: btypes.PropObjectName, Data: float32(1)},
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, err := extractObjectMetadata(devID, objID, tt.reply)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != "AI-1" {
				t.Fatalf("expected name AI-1 got %s", name)
			}
		})
	}
}

func TestExtractObjectIDsForRange(t *testing.T) {
	devID := btypes.ObjectID{Type: btypes.DeviceType, Instance: 617}

	t.Run("indexed response order independent", func(t *testing.T) {
		props := []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 3, Data: btypes.ObjectID{Type: btypes.BinaryInput, Instance: 3}},
			{Type: btypes.PropObjectList, ArrayIndex: 1, Data: btypes.ObjectID{Type: btypes.BinaryInput, Instance: 1}},
			{Type: btypes.PropObjectList, ArrayIndex: 2, Data: btypes.ObjectID{Type: btypes.BinaryInput, Instance: 2}},
		}
		objs, err := extractObjectIDsForRange(devID, 1, 3, props)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i, obj := range objs {
			want := uint32(i + 1)
			if uint32(obj.ID.Instance) != want {
				t.Fatalf("expected instance %d got %d", want, obj.ID.Instance)
			}
		}
	})

	t.Run("non indexed sequential response", func(t *testing.T) {
		props := []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: btypes.ArrayAll, Data: btypes.ObjectID{Type: btypes.BinaryValue, Instance: 4}},
			{Type: btypes.PropObjectList, ArrayIndex: btypes.ArrayAll, Data: btypes.ObjectID{Type: btypes.BinaryValue, Instance: 5}},
		}
		objs, err := extractObjectIDsForRange(devID, 4, 5, props)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(objs) != 2 {
			t.Fatalf("expected 2 objs got %d", len(objs))
		}
	})

	t.Run("wrong property type", func(t *testing.T) {
		props := []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 1, Data: float32(1)},
		}
		_, err := extractObjectIDsForRange(devID, 1, 1, props)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestExtractObjectListLength(t *testing.T) {
	devID := btypes.ObjectID{Type: btypes.DeviceType, Instance: 716}

	t.Run("uint32 list length", func(t *testing.T) {
		n, err := extractObjectListLength(devID, []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 0, Data: uint32(42)},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 42 {
			t.Fatalf("expected 42 got %d", n)
		}
	})

	t.Run("read property must return exactly one property", func(t *testing.T) {
		_, err := extractObjectListLength(devID, []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 0, Data: uint32(1)},
			{Type: btypes.PropObjectList, ArrayIndex: 0, Data: uint32(2)},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("float32 list length rejected", func(t *testing.T) {
		_, err := extractObjectListLength(devID, []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 0, Data: float32(42)},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid list length type", func(t *testing.T) {
		_, err := extractObjectListLength(devID, []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 0, Data: "not-a-length"},
		})
		if err == nil {
			t.Fatal("expected error for wrong list length type")
		}
	})
}

func TestObjectInformationPartialMetadataFailureDoesNotAbortDevice(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	c := cli.(*client)
	t.Cleanup(func() { _ = c.Close() })
	go c.ClientRun()

	devID := btypes.ObjectID{Type: btypes.DeviceType, Instance: 1234}
	addr := btypes.Address{Net: 1, Adr: []uint8{2}, Len: 1}
	dev := btypes.Device{
		ID:      devID,
		Addr:    addr,
		MaxApdu: 480,
		Objects: make(btypes.ObjectMap),
	}

	ai1 := btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1}
	ai2 := btypes.ObjectID{Type: btypes.AnalogInput, Instance: 2}
	dev.Objects[btypes.AnalogInput] = map[btypes.ObjectInstance]btypes.Object{
		1: {ID: ai1},
		2: {ID: ai2},
	}

	// Reply where AI:1 has valid metadata, but AI:2 has invalid metadata (wrong data type for name)
	reply := []btypes.Object{
		{
			ID: ai1,
			Properties: []btypes.Property{
				{Type: btypes.PropObjectName, Data: "Supply Temp"},
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			},
		},
		{
			ID: ai2,
			Properties: []btypes.Property{
				{Type: btypes.PropObjectName, Data: float32(123)}, // invalid string type
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			},
		},
	}

	link.Inject(datalink.ReceivedMessage{
		Data: readMultipleAckPacket(t, &dev.Addr, reply, -1),
		Src:  &dev.Addr,
	})

	err = c.objectInformation(&dev, []btypes.Object{{ID: ai1}, {ID: ai2}})
	require.NoError(t, err)

	// AI:1 should have received its name
	assert.Equal(t, "Supply Temp", dev.Objects[btypes.AnalogInput][1].Name)
	// AI:2 should remain in dev.Objects with an empty name instead of failing the whole discovery
	assert.Equal(t, "", dev.Objects[btypes.AnalogInput][2].Name)
	assert.Equal(t, ai2, dev.Objects[btypes.AnalogInput][2].ID)
}
