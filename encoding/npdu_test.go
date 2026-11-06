package encoding

import (
	"encoding/binary"
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/btypes/ndpu"
)

func TestIAmRouterToNetworkList(t *testing.T) {
	tests := []struct {
		name string
		nets []uint16
		want []uint16
	}{
		{name: "one network", nets: []uint16{2001}},
		{name: "two networks", nets: []uint16{2001, 2002}},
		{name: "three networks", nets: []uint16{2001, 2002, 2003}},
		{name: "duplicate network", nets: []uint16{2001, 2001, 2002}, want: []uint16{2001, 2002}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte{0x01, 0x80, byte(ndpu.IamRouterToNetwork)}
			for _, netNum := range tc.nets {
				var pair [2]byte
				binary.BigEndian.PutUint16(pair[:], netNum)
				raw = append(raw, pair[:]...)
			}
			want := tc.want
			if want == nil {
				want = tc.nets
			}

			var npdu btypes.NPDU
			list, err := NewDecoder(raw).NPDU(&npdu)
			if err != nil {
				t.Fatalf("NPDU: %v", err)
			}
			if len(list) != len(want) {
				t.Fatalf("networks %d, want %d", len(list), len(want))
			}
			for i, netNum := range want {
				if list[i].Net != netNum {
					t.Fatalf("network[%d] = %d, want %d", i, list[i].Net, netNum)
				}
			}
		})
	}
}
