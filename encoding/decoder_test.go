package encoding

import "testing"

func TestSigned24(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want int32
	}{
		{name: "minus one", raw: []byte{0xFF, 0xFF, 0xFF}, want: -1},
		{name: "minus 40000", raw: []byte{0xFF, 0x63, 0xC0}, want: -40000},
		{name: "zero", raw: []byte{0x00, 0x00, 0x00}, want: 0},
		{name: "100000", raw: []byte{0x01, 0x86, 0xA0}, want: 100000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDecoder(tc.raw)
			got := d.signed24()
			if err := d.Error(); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("signed24 = %d, want %d", got, tc.want)
			}
		})
	}
}
