package bacnet

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/btypes/ndpu"
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

	id, err := c.tsm.ID(t.Context(), &dev.Addr, btypes.ServiceConfirmedReadProperty)
	require.NoError(t, err)
	defer c.tsm.Put(id)

	done := make(chan error, 1)
	go func() {
		done <- c.tsm.Send(udpAddr, id, nil, "ok")
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

	id, err := c.tsm.ID(t.Context(), &dev.Addr, btypes.ServiceConfirmedReadProperty)
	require.NoError(t, err)
	defer c.tsm.Put(id)

	done := make(chan error, 1)
	go func() {
		done <- c.tsm.Send(folded, id, nil, "ok")
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

	id, err := c.tsm.ID(t.Context(), expected, btypes.ServiceConfirmedWriteProperty)
	require.NoError(t, err)
	defer c.tsm.Put(id)

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

func newTestClient(t *testing.T) *client {
	t.Helper()
	link := datalink.NewMockDataLink()
	cli, err := NewClient(&ClientBuilder{
		DataLink: link,
		Ip:       "192.168.0.165",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	return cli.(*client)
}

func udpAddr(ip net.IP, port int) *btypes.Address {
	return datalink.UDPToAddress(&net.UDPAddr{IP: ip.To4(), Port: port})
}

func routedPeer(router *btypes.Address, netNum uint16, mac uint8) *btypes.Address {
	return addressWithNPDUSource(router, &btypes.NPDU{
		Source: &btypes.Address{Net: netNum, Len: 1, Adr: []uint8{mac}},
	})
}

func station(netNum uint16, mac uint8) *btypes.Address {
	return &btypes.Address{Net: netNum, Len: 1, Adr: []uint8{mac}}
}

func captureLog(c *client) *bytes.Buffer {
	var buf bytes.Buffer
	c.log.SetOutput(&buf)
	c.log.SetLevel(log.DebugLevel)
	c.log.SetFormatter(&log.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})
	return &buf
}

func pendingReply(t *testing.T, c *client, peer *btypes.Address, service btypes.ServiceConfirmed) int {
	t.Helper()
	id, err := c.tsm.ID(t.Context(), peer, service)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.tsm.Put(id) })
	return id
}

func assertStillPending(t *testing.T, c *client, id int) {
	t.Helper()
	_, err := c.tsm.Receive(id, 30*time.Millisecond)
	require.Error(t, err)
}

func deliverReply(t *testing.T, c *client, id int, send func()) []byte {
	t.Helper()
	v := awaitReply(t, c, id, send)
	require.IsType(t, []byte{}, v)
	return v.([]byte)
}

func complexAckWith(t *testing.T, invokeID uint8, service btypes.ServiceConfirmed, npduSrc *btypes.Address, body []byte) []byte {
	t.Helper()
	apdu := append([]byte{byte(btypes.ComplexAck), invokeID, byte(service)}, body...)
	return bvlcWithNPDU(t, btypes.BacFuncUnicast, npduSrc, nil, apdu)
}

func TestSameInvokeIDDifferentPeers(t *testing.T) {
	router := udpAddr(net.IPv4(192, 168, 0, 78), datalink.DefaultPort)
	peer22 := routedPeer(router, 7, 22)
	peer5 := routedPeer(router, 7, 5)

	c1 := newTestClient(t)
	c2 := newTestClient(t)
	id1 := pendingReply(t, c1, peer22, btypes.ServiceConfirmedReadPropMultiple)
	id2 := pendingReply(t, c2, peer5, btypes.ServiceConfirmedReadPropMultiple)
	require.Equal(t, id1, id2, "each client starts its own invoke-id sequence")

	pkt22 := complexAckWith(t, uint8(id1), btypes.ServiceConfirmedReadPropMultiple, station(7, 22), []byte("mac-22"))
	pkt5 := complexAckWith(t, uint8(id2), btypes.ServiceConfirmedReadPropMultiple, station(7, 5), []byte("mac-5"))

	c1.handleMsg(router, pkt5)
	assertStillPending(t, c1, id1)
	c2.handleMsg(router, pkt22)
	assertStillPending(t, c2, id2)

	got1 := deliverReply(t, c1, id1, func() { c1.handleMsg(router, pkt22) })
	got2 := deliverReply(t, c2, id2, func() { c2.handleMsg(router, pkt5) })
	assert.Contains(t, string(got1), "mac-22")
	assert.NotContains(t, string(got1), "mac-5")
	assert.Contains(t, string(got2), "mac-5")
	assert.NotContains(t, string(got2), "mac-22")
}

func TestForeignReplyLeavesTransactionPending(t *testing.T) {
	router := udpAddr(net.IPv4(192, 168, 0, 78), datalink.DefaultPort)
	peer := routedPeer(router, 7, 22)

	cases := []struct {
		name   string
		packet func(id uint8) []byte
		src    *btypes.Address
	}{
		{
			name: "wrong router ip",
			src:  udpAddr(net.IPv4(10, 0, 0, 1), datalink.DefaultPort),
			packet: func(id uint8) []byte {
				return complexAckWith(t, id, btypes.ServiceConfirmedReadPropMultiple, station(7, 22), []byte("other-ip"))
			},
		},
		{
			name: "wrong router port",
			src:  udpAddr(net.IPv4(192, 168, 0, 78), datalink.DefaultPort+1),
			packet: func(id uint8) []byte {
				return complexAckWith(t, id, btypes.ServiceConfirmedReadPropMultiple, station(7, 22), []byte("other-port"))
			},
		},
		{
			name: "wrong routed sadr",
			src:  router,
			packet: func(id uint8) []byte {
				return complexAckWith(t, id, btypes.ServiceConfirmedReadPropMultiple, station(7, 5), []byte("other-mac"))
			},
		},
		{
			name: "wrong service",
			src:  router,
			packet: func(id uint8) []byte {
				return complexAckWith(t, id, btypes.ServiceConfirmedReadProperty, station(7, 22), []byte("other-service"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			buf := captureLog(c)
			id := pendingReply(t, c, peer, btypes.ServiceConfirmedReadPropMultiple)
			c.handleMsg(tc.src, tc.packet(uint8(id)))
			assertStillPending(t, c, id)
			assert.NotContains(t, buf.String(), "level=error")

			own := complexAckWith(t, uint8(id), btypes.ServiceConfirmedReadPropMultiple, station(7, 22), []byte("present-value"))
			got := deliverReply(t, c, id, func() { c.handleMsg(router, own) })
			assert.Contains(t, string(got), "present-value")
			assert.NotContains(t, buf.String(), "level=error")
		})
	}
}

func TestForwardedNPDUUsesOrigin(t *testing.T) {
	c := newTestClient(t)
	buf := captureLog(c)
	bbmd := udpAddr(net.IPv4(192, 168, 1, 1), datalink.DefaultPort)
	origin := udpAddr(net.IPv4(10, 0, 0, 5), datalink.DefaultPort)
	npduSrc := station(7, 22)

	bbmdID := pendingReply(t, c, bbmd, btypes.ServiceConfirmedReadProperty)
	c.handleMsg(bbmd, forwardedNPDUPacket(t, uint8(bbmdID), append([]byte(nil), origin.Mac...), npduSrc))
	assertStillPending(t, c, bbmdID)

	expected := addressWithNPDUSource(origin, &btypes.NPDU{Source: npduSrc})
	id := pendingReply(t, c, expected, btypes.ServiceConfirmedReadProperty)
	got := deliverReply(t, c, id, func() {
		c.handleMsg(bbmd, forwardedNPDUPacket(t, uint8(id), append([]byte(nil), origin.Mac...), npduSrc))
	})
	require.NotEmpty(t, got)
	assert.NotContains(t, buf.String(), "level=error")
	assert.NotContains(t, buf.String(), "Issue decoding APDU")
}

func TestRejectAndAbortFollowThePendingRequest(t *testing.T) {
	router := udpAddr(net.IPv4(192, 168, 0, 78), datalink.DefaultPort)
	peer := routedPeer(router, 7, 22)
	cases := []struct {
		name         string
		src          *btypes.Address
		pdu          btypes.PDUType
		reason       byte
		service      btypes.ServiceConfirmed
		errorService btypes.ServiceConfirmed
		want         string
	}{
		{"reject from another source", udpAddr(net.IPv4(10, 0, 0, 1), datalink.DefaultPort), btypes.Reject, 1, btypes.ServiceConfirmedReadPropMultiple, 0, ""},
		{"matching reject", router, btypes.Reject, 9, btypes.ServiceConfirmedReadPropMultiple, 0, "reject reason 9"},
		{"matching abort", router, btypes.Abort, 1, btypes.ServiceConfirmedWriteProperty, 0, "abort reason 1"},
		{"error wrong service", router, btypes.Error, 0, btypes.ServiceConfirmedReadPropMultiple, btypes.ServiceConfirmedReadProperty, ""},
		{"matching error", router, btypes.Error, 0, btypes.ServiceConfirmedReadPropMultiple, btypes.ServiceConfirmedReadPropMultiple, "error class"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			buf := captureLog(c)
			id := pendingReply(t, c, peer, tc.service)
			apdu := []byte{byte(tc.pdu), uint8(id), tc.reason}
			if tc.pdu == btypes.Error {
				// Application enumerated tags. The decoder stores the tag meta as class and code.
				apdu = []byte{byte(btypes.Error), uint8(id), byte(tc.errorService), 0x91, 0x01, 0x91, 0x02}
			}
			pkt := pduPacket(t, apdu, station(7, 22))
			if tc.want == "" {
				c.handleMsg(tc.src, pkt)
				assertStillPending(t, c, id)
				got := deliverReply(t, c, id, func() {
					c.handleMsg(router, complexAckWith(t, uint8(id), tc.service, station(7, 22), []byte("present-value")))
				})
				assert.Contains(t, string(got), "present-value")
				assert.NotContains(t, buf.String(), "level=error")
				return
			}
			got := awaitReply(t, c, id, func() { c.handleMsg(tc.src, pkt) })
			err, ok := got.(error)
			require.True(t, ok, "got %T", got)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, buf.String(), "level=error")
		})
	}
}

func awaitReply(t *testing.T, c *client, id int, send func()) interface{} {
	t.Helper()
	done := make(chan interface{}, 1)
	go func() {
		raw, err := c.tsm.Receive(id, time.Second)
		if err != nil {
			done <- err
			return
		}
		done <- raw
	}()
	send()
	select {
	case v := <-done:
		return v
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reply")
	}
	return nil
}

func pduPacket(t *testing.T, apdu []byte, npduSrc *btypes.Address) []byte {
	t.Helper()
	return bvlcWithNPDU(t, btypes.BacFuncUnicast, npduSrc, nil, apdu)
}

func TestNetworkLayerMessageDoesNotDecodeAPDU(t *testing.T) {
	c := newTestClient(t)
	buf := captureLog(c)
	src := udpAddr(net.IPv4(192, 168, 0, 78), datalink.DefaultPort)
	c.handleMsg(src, networkLayerPacket(t))
	assert.Contains(t, buf.String(), "Ignored Network Layer Message")
	assert.NotContains(t, buf.String(), "Issue decoding APDU")
	assert.NotContains(t, buf.String(), "level=error")
}

func networkLayerPacket(t *testing.T) []byte {
	t.Helper()
	payload := encoding.NewEncoder()
	payload.NPDU(&btypes.NPDU{
		Version:                 btypes.ProtocolVersion,
		IsNetworkLayerMessage:   true,
		NetworkLayerMessageType: ndpu.WhoIsRouterToNetwork,
	})
	require.NoError(t, payload.Error())
	enc := encoding.NewEncoder()
	err := enc.BVLC(btypes.BVLC{
		Type:     btypes.BVLCTypeBacnetIP,
		Function: btypes.BacFuncBroadcast,
		Length:   4 + uint16(len(payload.Bytes())),
		Data:     payload.Bytes(),
	})
	require.NoError(t, err)
	require.NoError(t, enc.Error())
	return enc.Bytes()
}
