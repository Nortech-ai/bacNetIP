package bacnet

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/Nortech-ai/bacNetIP/encoding"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientRunEOF(t *testing.T) {
	link := datalink.NewMockDataLink()
	cb := &ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	}

	c, err := NewClient(cb)
	assert.NoError(t, err)
	defer c.Close()

	link.Received = []datalink.ReceivedMessage{
		{
			Error: fmt.Errorf("Hey! I'm an error!"),
		},
		{
			Error: io.EOF,
		},
	}

	// This should only exit if we get an EOF error
	c.ClientRun()

	// So if we got here the test succeeded. This tests that we both handled the first
	// error and continued processing messages, and that we exited the when we got an EOF
}

func TestHandleMsgUnconfirmedDoesNotError(t *testing.T) {
	cases := []struct {
		name    string
		service btypes.ServiceUnconfirmed
	}{
		{name: "COVNotification", service: btypes.ServiceUnconfirmedCOVNotification},
		{name: "TimeSynchronization", service: btypes.ServiceUnconfirmedTimeSync},
		{name: "UTCTimeSynchronization", service: btypes.ServiceUnconfirmedUTCTimeSync},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := datalink.NewMockDataLink()
			cli, err := NewClient(&ClientBuilder{
				DataLink: link,
				Ip:       "192.168.1.100",
			})
			require.NoError(t, err)
			defer cli.Close()

			c := cli.(*client)
			var buf bytes.Buffer
			c.log.SetOutput(&buf)
			c.log.SetLevel(log.DebugLevel)
			c.log.SetFormatter(&log.TextFormatter{
				DisableColors:    true,
				DisableTimestamp: true,
			})

			src := &btypes.Address{Mac: []byte{192, 168, 1, 10, 0xBA, 0xC0}}
			c.handleMsg(src, unconfirmedServicePacket(t, tc.service))

			out := buf.String()
			assert.NotContains(t, out, "level=error")
			assert.Contains(t, out, fmt.Sprintf("Ignoring unconfirmed service %d", tc.service))
		})
	}
}

func unconfirmedServicePacket(t *testing.T, service btypes.ServiceUnconfirmed) []byte {
	t.Helper()

	payload := encoding.NewEncoder()
	payload.NPDU(&btypes.NPDU{
		Version: btypes.ProtocolVersion,
	})
	err := payload.APDU(btypes.APDU{
		DataType:           btypes.UnconfirmedServiceRequest,
		UnconfirmedService: service,
	})
	require.NoError(t, err)
	require.NoError(t, payload.Error())

	data := payload.Bytes()
	enc := encoding.NewEncoder()
	err = enc.BVLC(btypes.BVLC{
		Type:     btypes.BVLCTypeBacnetIP,
		Function: btypes.BacFuncBroadcast,
		Length:   4 + uint16(len(data)),
		Data:     data,
	})
	require.NoError(t, err)
	require.NoError(t, enc.Error())
	return enc.Bytes()
}

func TestAddressWithNPDUSource(t *testing.T) {
	router := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1).To4(),
		Port: 0xBAC0,
	})
	npdu := &btypes.NPDU{
		Source: &btypes.Address{
			Net: 2001,
			Len: 1,
			Adr: []uint8{0x1D},
		},
	}

	got := addressWithNPDUSource(router, npdu)
	require.NotNil(t, got)
	assert.Equal(t, uint16(2001), got.Net)
	assert.Equal(t, uint8(1), got.Len)
	assert.Equal(t, []uint8{0x1D}, got.Adr)
	assert.Equal(t, router.Mac, got.Mac)
	assert.Equal(t, router.MacLen, got.MacLen)

	// Datalink-only input stays Mac/MacLen when there is no NPDU source.
	bare := addressWithNPDUSource(router, &btypes.NPDU{})
	require.NotNil(t, bare)
	assert.Equal(t, uint16(0), bare.Net)
	assert.Empty(t, bare.Adr)
	assert.Equal(t, router.Mac, bare.Mac)
}

func TestHandleMsgRoutedComplexAckMatchesStoredAddress(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	defer cli.Close()
	c := cli.(*client)

	routerUDP := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1).To4(),
		Port: 0xBAC0,
	})
	expected := addressWithNPDUSource(routerUDP, &btypes.NPDU{
		Source: &btypes.Address{Net: 2001, Len: 1, Adr: []uint8{0x1D}},
	})

	id, err := c.tsm.ID(t.Context())
	require.NoError(t, err)
	defer c.tsm.Put(id)
	require.NoError(t, c.tsm.ExpectSource(id, expected))

	done := make(chan interface{}, 1)
	go func() {
		raw, recvErr := c.tsm.Receive(id, time.Second)
		if recvErr != nil {
			done <- recvErr
			return
		}
		done <- raw
	}()

	// Incoming datalink source is UDP-only; NPDU carries Net + Adr.
	c.handleMsg(routerUDP, complexAckPacket(t, uint8(id), &btypes.Address{
		Net: 2001,
		Len: 1,
		Adr: []uint8{0x1D},
	}))

	select {
	case v := <-done:
		require.IsType(t, []byte{}, v)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for routed complex ack delivery")
	}
}

func TestHandleMsgRoutedComplexAckRejectsWrongRouter(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	defer cli.Close()
	c := cli.(*client)

	routerUDP := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1).To4(),
		Port: 0xBAC0,
	})
	expected := addressWithNPDUSource(routerUDP, &btypes.NPDU{
		Source: &btypes.Address{Net: 2001, Len: 1, Adr: []uint8{0x1D}},
	})

	id, err := c.tsm.ID(t.Context())
	require.NoError(t, err)
	defer c.tsm.Put(id)
	require.NoError(t, c.tsm.ExpectSource(id, expected))

	wrongRouter := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(10, 0, 0, 1).To4(),
		Port: 0xBAC0,
	})
	c.handleMsg(wrongRouter, complexAckPacket(t, uint8(id), &btypes.Address{
		Net: 2001,
		Len: 1,
		Adr: []uint8{0x1D},
	}))

	_, err = c.tsm.Receive(id, 50*time.Millisecond)
	require.Error(t, err, "reply from a different UDP sender must not be delivered")
}

func TestNewDeviceAddressMatchesUDPToAddress(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	defer cli.Close()
	c := cli.(*client)

	dev, err := btypes.NewDevice(&btypes.Device{
		Ip:            "10.0.0.20",
		Port:          0xBAC0,
		NetworkNumber: 0,
		MacMSTP:       0,
		ID:            btypes.ObjectID{Type: btypes.DeviceType, Instance: 1},
	})
	require.NoError(t, err)

	udpAddr := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(10, 0, 0, 20).To4(),
		Port: 0xBAC0,
	})

	id, err := c.tsm.ID(t.Context())
	require.NoError(t, err)
	defer c.tsm.Put(id)
	require.NoError(t, c.tsm.ExpectSource(id, &dev.Addr))

	done := make(chan error, 1)
	go func() {
		done <- c.tsm.SendFrom(udpAddr, id, "ok")
	}()
	raw, err := c.tsm.Receive(id, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "ok", raw)
	require.NoError(t, <-done)
}

func TestNewDeviceRoutedAddressMatchesFoldedSource(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	defer cli.Close()
	c := cli.(*client)

	dev, err := btypes.NewDevice(&btypes.Device{
		Ip:            "192.168.1.1",
		Port:          0xBAC0,
		NetworkNumber: 2001,
		MacMSTP:       0x1D,
		ID:            btypes.ObjectID{Type: btypes.DeviceType, Instance: 2},
	})
	require.NoError(t, err)

	routerUDP := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1).To4(),
		Port: 0xBAC0,
	})
	folded := addressWithNPDUSource(routerUDP, &btypes.NPDU{
		Source: &btypes.Address{Net: 2001, Len: 1, Adr: []uint8{0x1D}},
	})

	id, err := c.tsm.ID(t.Context())
	require.NoError(t, err)
	defer c.tsm.Put(id)
	require.NoError(t, c.tsm.ExpectSource(id, &dev.Addr))

	done := make(chan error, 1)
	go func() {
		done <- c.tsm.SendFrom(folded, id, "ok")
	}()
	raw, err := c.tsm.Receive(id, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "ok", raw)
	require.NoError(t, <-done)
}

func TestHandleMsgRoutedSimpleAckMatchesStoredAddress(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	defer cli.Close()
	c := cli.(*client)

	routerUDP := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1).To4(),
		Port: 0xBAC0,
	})
	expected := addressWithNPDUSource(routerUDP, &btypes.NPDU{
		Source: &btypes.Address{Net: 2001, Len: 1, Adr: []uint8{0x1D}},
	})

	id, err := c.tsm.ID(t.Context())
	require.NoError(t, err)
	defer c.tsm.Put(id)
	require.NoError(t, c.tsm.ExpectSource(id, expected))

	done := make(chan interface{}, 1)
	go func() {
		raw, recvErr := c.tsm.Receive(id, time.Second)
		if recvErr != nil {
			done <- recvErr
			return
		}
		done <- raw
	}()

	c.handleMsg(routerUDP, simpleAckPacket(t, uint8(id), &btypes.Address{
		Net: 2001,
		Len: 1,
		Adr: []uint8{0x1D},
	}))

	select {
	case v := <-done:
		require.IsType(t, []byte{}, v)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for routed simple ack delivery")
	}
}

func TestHandleMsgForwardedNPDUDecodesAPDU(t *testing.T) {
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	})
	require.NoError(t, err)
	defer cli.Close()
	c := cli.(*client)

	var buf bytes.Buffer
	c.log.SetOutput(&buf)
	c.log.SetLevel(log.DebugLevel)
	c.log.SetFormatter(&log.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})

	bbmd := datalink.UDPToAddress(&net.UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1).To4(),
		Port: 0xBAC0,
	})
	expected := addressWithNPDUSource(bbmd, &btypes.NPDU{})

	id, err := c.tsm.ID(t.Context())
	require.NoError(t, err)
	defer c.tsm.Put(id)
	require.NoError(t, c.tsm.ExpectSource(id, expected))

	done := make(chan interface{}, 1)
	go func() {
		raw, recvErr := c.tsm.Receive(id, time.Second)
		if recvErr != nil {
			done <- recvErr
			return
		}
		done <- raw
	}()

	origin := []byte{10, 0, 0, 5, 0xBA, 0xC0} // dummy original BACnet/IP address
	c.handleMsg(bbmd, forwardedNPDUPacket(t, uint8(id), origin, nil))

	select {
	case v := <-done:
		require.IsType(t, []byte{}, v, "forwarded complex ack should deliver")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded-npdu delivery")
	}

	out := buf.String()
	assert.NotContains(t, out, "Issue decoding APDU")
	assert.NotContains(t, out, "Ignored NDPU Forwarded")
}

func complexAckPacket(t *testing.T, invokeID uint8, npduSrc *btypes.Address) []byte {
	t.Helper()
	return ackPacket(t, btypes.BacFuncUnicast, btypes.ComplexAck, invokeID, nil, npduSrc)
}

func simpleAckPacket(t *testing.T, invokeID uint8, npduSrc *btypes.Address) []byte {
	t.Helper()
	// Encoder.APDU does not encode SimpleAck; layout is PDU type + invoke + service.
	apdu := []byte{byte(btypes.SimpleAck), invokeID, byte(btypes.ServiceConfirmedWriteProperty)}
	return bvlcWithNPDU(t, btypes.BacFuncUnicast, npduSrc, nil, apdu)
}

func forwardedNPDUPacket(t *testing.T, invokeID uint8, origin []byte, npduSrc *btypes.Address) []byte {
	t.Helper()
	require.Len(t, origin, forwardedNPDUOriginLength)
	return ackPacket(t, btypes.BacFuncForwardedNPDU, btypes.ComplexAck, invokeID, origin, npduSrc)
}

func ackPacket(t *testing.T, bacFunc btypes.BacFunc, dataType btypes.PDUType, invokeID uint8, origin []byte, npduSrc *btypes.Address) []byte {
	t.Helper()

	payload := encoding.NewEncoder()
	err := payload.APDU(btypes.APDU{
		DataType: dataType,
		InvokeId: invokeID,
		Service:  btypes.ServiceConfirmedReadProperty,
	})
	require.NoError(t, err)
	require.NoError(t, payload.Error())
	return bvlcWithNPDU(t, bacFunc, npduSrc, origin, payload.Bytes())
}

func bvlcWithNPDU(t *testing.T, bacFunc btypes.BacFunc, npduSrc *btypes.Address, origin []byte, apdu []byte) []byte {
	t.Helper()

	payload := encoding.NewEncoder()
	payload.NPDU(&btypes.NPDU{
		Version: btypes.ProtocolVersion,
		Source:  npduSrc,
	})
	require.NoError(t, payload.Error())
	data := append(payload.Bytes(), apdu...)
	if len(origin) > 0 {
		data = append(append([]byte(nil), origin...), data...)
	}
	enc := encoding.NewEncoder()
	err := enc.BVLC(btypes.BVLC{
		Type:     btypes.BVLCTypeBacnetIP,
		Function: bacFunc,
		Length:   4 + uint16(len(data)),
		Data:     data,
	})
	require.NoError(t, err)
	require.NoError(t, enc.Error())
	return enc.Bytes()
}
