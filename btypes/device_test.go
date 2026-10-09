package btypes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDevice_DirectAddress(t *testing.T) {
	dev, err := NewDevice(&Device{
		Ip:            "192.168.1.50",
		Port:          0xBAC0,
		NetworkNumber: 0,
		MacMSTP:       0,
		ID:            ObjectID{Type: DeviceType, Instance: 100},
	})
	require.NoError(t, err)
	require.NotNil(t, dev)

	assert.Equal(t, uint16(0), dev.Addr.Net)
	assert.Equal(t, uint8(6), dev.Addr.MacLen)
	assert.Equal(t, []uint8{192, 168, 1, 50, 0xBA, 0xC0}, dev.Addr.Mac)
	assert.Empty(t, dev.Addr.Adr)
	assert.Equal(t, uint8(0), dev.Addr.Len)
}

func TestNewDevice_RoutedAddress(t *testing.T) {
	dev, err := NewDevice(&Device{
		Ip:            "192.168.1.1",
		Port:          0xBAC0,
		NetworkNumber: 2001,
		MacMSTP:       0x1D,
		ID:            ObjectID{Type: DeviceType, Instance: 200},
	})
	require.NoError(t, err)
	require.NotNil(t, dev)

	assert.Equal(t, uint16(2001), dev.Addr.Net)
	assert.Equal(t, uint8(6), dev.Addr.MacLen)
	assert.Equal(t, []uint8{192, 168, 1, 1, 0xBA, 0xC0}, dev.Addr.Mac)
	assert.Equal(t, []uint8{0x1D}, dev.Addr.Adr)
	assert.Equal(t, uint8(1), dev.Addr.Len)
}

func TestSetLengthUsesAdrLength(t *testing.T) {
	mstp := Address{Adr: []uint8{22}}
	mstp.SetLength()
	assert.Equal(t, uint8(1), mstp.Len)

	ipMAC := Address{Adr: []uint8{192, 168, 0, 78, 0xBA, 0xC0}}
	ipMAC.SetLength()
	assert.Equal(t, uint8(6), ipMAC.Len)

	ipMAC.SetLength()
	assert.Equal(t, uint8(6), ipMAC.Len)
}
