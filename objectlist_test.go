package bacnet

import (
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

func TestExtractObjectMetadata(t *testing.T) {
	devID := btypes.ObjectID{Type: btypes.DeviceType, Instance: 607}
	objID := btypes.ObjectID{Type: btypes.AnalogInput, Instance: 1}

	tests := []struct {
		name    string
		props   []btypes.Property
		wantErr bool
	}{
		{
			name: "swapped property order",
			props: []btypes.Property{
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
				{Type: btypes.PropObjectName, Data: "AI-1"},
			},
		},
		{
			name: "extra properties ignored",
			props: []btypes.Property{
				{Type: btypes.PropDescription, Data: "desc"},
				{Type: btypes.PropObjectName, Data: "AI-1"},
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			},
		},
		{
			name: "object type encoded as float32 (gateway quirk)",
			props: []btypes.Property{
				{Type: btypes.PropObjectType, Data: float32(btypes.AnalogInput)},
				{Type: btypes.PropObjectName, Data: "AI-1"},
			},
		},
		{
			name: "missing object name",
			props: []btypes.Property{
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			},
			wantErr: true,
		},
		{
			name: "wrong object name type",
			props: []btypes.Property{
				{Type: btypes.PropObjectName, Data: float32(1)},
				{Type: btypes.PropObjectType, Data: uint32(btypes.AnalogInput)},
			},
			wantErr: true,
		},
		{
			name: "wrong object type type",
			props: []btypes.Property{
				{Type: btypes.PropObjectName, Data: "AI-1"},
				{Type: btypes.PropObjectType, Data: "not-an-object-type"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, typ, err := extractObjectMetadata(devID, objID, tt.props)
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
			if typ != btypes.AnalogInput {
				t.Fatalf("expected type AnalogInput got %v", typ)
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

	t.Run("float32 coerced to length", func(t *testing.T) {
		n, err := extractObjectListLength(devID, []btypes.Property{
			{Type: btypes.PropObjectList, ArrayIndex: 0, Data: float32(42)},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 42 {
			t.Fatalf("expected 42 got %d", n)
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
