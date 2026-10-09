package bacnet

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/btypes/ndpu"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/Nortech-ai/bacNetIP/encoding"
	"github.com/Nortech-ai/bacNetIP/helpers/validation"
	"github.com/Nortech-ai/bacNetIP/tsm"
	"github.com/Nortech-ai/bacNetIP/utsm"
	log "github.com/sirupsen/logrus"
)

const mtuHeaderLength = 4
const defaultStateSize = 20
const forwardedNPDUOriginLength = 6 // Annex J: original BACnet/IP address after BVLC

type Client interface {
	io.Closer
	ClientClose(closeLogs bool) error
	ClientRun()
	WhoIs(wh *WhoIsOpts) ([]btypes.Device, error)
	WhatIsNetworkNumber() []*btypes.Address
	IAm(dest btypes.Address, iam btypes.IAm) error
	WhoIsRouterToNetwork() (resp *[]btypes.Address)
	Objects(dev btypes.Device) (btypes.Device, error)
	ReadProperty(dest btypes.Device, rp btypes.PropertyData) (btypes.PropertyData, error)
	ReadMultiProperty(dev btypes.Device, rp btypes.MultiplePropertyData) (btypes.MultiplePropertyData, error)
	WriteProperty(dest btypes.Device, wp btypes.PropertyData) error
	WriteMultiProperty(dev btypes.Device, wp btypes.MultiplePropertyData) error
}

type client struct {
	dataLink       datalink.DataLink
	tsm            *tsm.TSM
	utsm           *utsm.Manager
	readBufferPool sync.Pool
	log            *log.Logger
}

type ClientBuilder struct {
	DataLink          datalink.DataLink
	Interface         string
	Ip                string
	Port              int
	SubnetCIDR        int
	MaxPDU            uint16
	LogLevel          *log.Level
	UsePcap           bool
	PcapListenTimeout time.Duration
}

// NewClient creates a new client with the given interface and
func NewClient(cb *ClientBuilder) (Client, error) {
	var err error
	var dataLink datalink.DataLink
	iface := cb.Interface
	ip := cb.Ip
	port := cb.Port
	maxPDU := cb.MaxPDU
	//check ip
	ok := validation.ValidIP(ip)
	if !ok {
		return nil, fmt.Errorf("invalid ip")
	}
	//check port
	if port == 0 {
		port = datalink.DefaultPort
	}
	ok = validation.ValidPort(port)
	if !ok {
		return nil, fmt.Errorf("invalid port")
	}
	//check adpu
	if maxPDU == 0 {
		maxPDU = btypes.MaxAPDU
	}
	//build datalink
	if cb.DataLink != nil {
		dataLink = cb.DataLink
	} else if cb.UsePcap {
		dataLink, err = datalink.NewPcapDataLink(iface, port, cb.PcapListenTimeout)
		if err != nil {
			return nil, fmt.Errorf("pcap datalink on %q: %w", iface, err)
		}
	} else if iface != "" {
		dataLink, err = datalink.NewUDPDataLink(iface, port)
		if err != nil {
			return nil, fmt.Errorf("udp datalink on %q: %w", iface, err)
		}
	} else {
		//check subnet
		sub := cb.SubnetCIDR
		ok = validation.ValidCIDR(ip, sub)
		if !ok {
			return nil, fmt.Errorf("invalid ip or subnet cidr")
		}
		dataLink, err = datalink.NewUDPDataLinkFromIP(ip, sub, port)
		if err != nil {
			return nil, fmt.Errorf("udp datalink on %s/%d: %w", ip, sub, err)
		}
	}

	l := log.New()
	l.Formatter = &log.TextFormatter{}
	if cb.LogLevel != nil {
		l.SetLevel(*cb.LogLevel)
	} else {
		l.SetLevel(log.DebugLevel)
	}

	cli := &client{
		dataLink: dataLink,
		tsm:      tsm.New(defaultStateSize),
		utsm: utsm.NewManager(
			utsm.DefaultSubscriberTimeout(time.Second*time.Duration(10)),
			utsm.DefaultSubscriberLastReceivedTimeout(time.Second*time.Duration(2)),
		),
		readBufferPool: sync.Pool{New: func() interface{} {
			return make([]byte, maxPDU)
		}},
		log: l,
	}
	return cli, err
}

func (c *client) ClientRun() {
	for {
		b := c.readBufferPool.Get().([]byte)
		addr, n, err := c.dataLink.Receive(b)

		// If the data link is closed, return
		if err == io.EOF {
			c.log.Error(fmt.Errorf("data link closed: %w", err))
			return
		}

		// Otherwise if we got an unknown error, continue.
		// Pcap drops datagrams for another local socket with an error here.
		if err != nil {
			continue
		}

		go c.handleMsg(addr, b[:n])
	}
}

func (c *client) handleMsg(src *btypes.Address, b []byte) {
	var header btypes.BVLC
	var npdu btypes.NPDU
	var apdu btypes.APDU
	dec := encoding.NewDecoder(b)
	err := dec.BVLC(&header)
	if err != nil {
		c.log.Error(err)
		return
	}

	if header.Function == btypes.BacFuncBroadcast || header.Function == btypes.BacFuncUnicast || header.Function == btypes.BacFuncForwardedNPDU {
		// Remove the BVLC header (decoder already advanced past it via BVLC()).
		b = b[mtuHeaderLength:]
		// Annex J Forwarded-NPDU: 4-byte BVLC + 6-octet original source, then NPDU.
		// The origin replaces the BBMD address before NPDU source is folded in.
		if header.Function == btypes.BacFuncForwardedNPDU {
			if len(b) < forwardedNPDUOriginLength {
				c.log.Error("forwarded-npdu missing original source address")
				return
			}
			// Annex J origin, not the BBMD. NPDU source still overlays SNET/SADR.
			origin := b[:forwardedNPDUOriginLength]
			b = b[forwardedNPDUOriginLength:]
			if err := dec.Skip(forwardedNPDUOriginLength); err != nil {
				c.log.Error(err)
				return
			}
			src = &btypes.Address{Mac: origin, MacLen: forwardedNPDUOriginLength}
		}
		networkList, err := dec.NPDU(&npdu)
		if err != nil {
			return
		}

		if npdu.IsNetworkLayerMessage {
			c.log.Debug("Ignored Network Layer Message")
			if npdu.NetworkLayerMessageType == ndpu.NetworkIs {
				c.utsm.Publish(int(npdu.Source.Net), npdu)
			}
			if npdu.NetworkLayerMessageType == ndpu.IamRouterToNetwork {
				c.utsm.Publish(int(npdu.Source.Net), networkList)
			}
			return
		}

		// We want to keep the APDU intact, so we will get a snapshot before decoding
		send := dec.Bytes()
		err = dec.APDU(&apdu)
		if err != nil {
			c.log.Errorf("Issue decoding APDU: %v", err)
			return
		}
		switch apdu.DataType {
		case btypes.UnconfirmedServiceRequest:
			switch apdu.UnconfirmedService {
			case btypes.ServiceUnconfirmedIAm:
				dec = encoding.NewDecoder(apdu.RawData)
				var iam btypes.IAm
				err = dec.IAm(&iam)
				c.log.Debug("Received IAM Message", iam.ID)
				iam.Addr = *addressWithNPDUSource(src, &npdu)
				if npdu.Source != nil {
					if npdu.Source.Net > 0 {
						c.log.Debug("device-network-address", npdu.Source.Net)
					}
					if len(npdu.Source.Adr) > 0 {
						c.log.Debug("device-mstp-mac-address", npdu.Source.Adr)
					}
				}
				if err != nil {
					c.log.Error(err)
					return
				}

				c.utsm.Publish(int(iam.ID.Instance), iam)
			case btypes.ServiceUnconfirmedWhoIs:
				dec := encoding.NewDecoder(apdu.RawData)
				var low, high int32
				dec.WhoIs(&low, &high)
				// For now we are going to ignore who is request.
				//log.WithFields(log.Fields{"low": low, "high": high}).Debug("WHO IS Request")
			default:
				// Foreign unconfirmed traffic on a shared BACnet/IP LAN (e.g. COV
				// notifications from other clients' subscriptions, time sync) is
				// expected when we only poll via ReadProperty. Do not treat as error.
				c.log.Debugf("Ignoring unconfirmed service %d", apdu.UnconfirmedService)
			}
		case btypes.SimpleAck:
			c.log.Debug("Received Simple Ack")
			svc := apdu.Service
			c.deliver(src, &npdu, apdu.InvokeId, &svc, send)
		case btypes.ComplexAck:
			// Segmented ComplexAck is unsupported; requests do not accept segmentation.
			c.log.Debug("Received Complex Ack")
			svc := apdu.Service
			c.deliver(src, &npdu, apdu.InvokeId, &svc, send)
		case btypes.Error:
			bacErr := fmt.Errorf("error class %s code %s", apdu.Error.Class.String(), apdu.Error.Code.String())
			svc := apdu.Service
			c.deliver(src, &npdu, apdu.InvokeId, &svc, bacErr)
		case btypes.Reject:
			rej := fmt.Errorf("reject reason %d", apdu.RejectReason)
			c.deliver(src, &npdu, apdu.InvokeId, nil, rej)
		case btypes.Abort:
			ab := fmt.Errorf("abort reason %d", apdu.AbortReason)
			c.deliver(src, &npdu, apdu.InvokeId, nil, ab)
		default:
			// Ignore it
			log.WithFields(log.Fields{"raw": b}).Debug("An ignored packet went through")
		}
	}
}

// deliver routes a TSM response by invoke-id to the waiter matching the
// NPDU-overlaid source and, when service is set, the confirmed-service choice.
// A mismatch is debug-only: another process's reply on a shared port is routine
// and must not be logged as an error or counted. The transaction stays pending.
func (c *client) deliver(src *btypes.Address, npdu *btypes.NPDU, invokeID uint8, service *btypes.ServiceConfirmed, payload interface{}) {
	if c.tsm.Send(addressWithNPDUSource(src, npdu), int(invokeID), service, payload) != nil {
		c.log.Debug("ignoring reply that does not match the pending request")
	}
}

// addressWithNPDUSource returns the datalink source with NPDU source network
// and MS/TP address overlaid when present. Datalink Receive only reports the
// UDP sender (Mac/MacLen); discoverer I-Am handling stores the same overlay on
// device.Addr, so reply correlation must apply it before matching.
func addressWithNPDUSource(src *btypes.Address, npdu *btypes.NPDU) *btypes.Address {
	if src == nil {
		return nil
	}
	addr := *src
	if src.Mac != nil {
		addr.Mac = append([]uint8(nil), src.Mac...)
	}
	if len(src.Adr) > 0 {
		addr.Adr = append([]uint8(nil), src.Adr...)
	}
	if npdu != nil && npdu.Source != nil {
		if npdu.Source.Net > 0 {
			addr.Net = npdu.Source.Net
		}
		if len(npdu.Source.Adr) > 0 {
			addr.Adr = append([]uint8(nil), npdu.Source.Adr...)
			addr.Len = npdu.Source.Len
			if addr.Len == 0 {
				addr.SetLength()
			}
		}
	}
	return &addr
}

type SetBroadcastType struct { //used to override the header.Function
	Set     bool
	BacFunc btypes.BacFunc
}

// Send transfers the raw apdu byte slice to the destination address.
func (c *client) Send(dest btypes.Address, npdu *btypes.NPDU, data []byte, broadcastType *SetBroadcastType) (int, error) {
	//broadcastType = &SetBroadcastType{}
	var header btypes.BVLC
	// Set packet type
	header.Type = btypes.BVLCTypeBacnetIP
	//if Adr is > 0 it must be an mst-tp device so send a UNICAST
	if len(dest.Adr) > 0 { //(aidan) not sure if this is correct, but it needs to be set to work to send (UNICAST) messages over a bacnet network
		// SET UNICAST FLAG
		// see http://www.bacnet.org/Tutorial/HMN-Overview/sld033.
		// see https://github.com/JoelBender/bacpypes/blob/9fca3f608a97a20807cd188689a2b9ff60b05085/doc/source/gettingstarted/gettingstarted001.rst#udp-communications-issues
		header.Function = btypes.BacFuncUnicast
	} else if dest.IsBroadcast() || dest.IsSubBroadcast() {
		// SET BROADCAST FLAG
		header.Function = btypes.BacFuncBroadcast
	} else {
		// SET UNICAST FLAG
		header.Function = btypes.BacFuncUnicast
	}

	if broadcastType != nil {
		if broadcastType.Set {
			header.Function = broadcastType.BacFunc
		}
	}

	header.Length = uint16(mtuHeaderLength + len(data))
	header.Data = data
	e := encoding.NewEncoder()
	err := e.BVLC(header)
	if err != nil {
		return 0, err
	}
	// use default udp type, src = network address (nil)
	return c.dataLink.Send(e.Bytes(), npdu, &dest)
}

func (c *client) ClientClose(closeLogs bool) error {
	if closeLogs {
		if f, ok := c.log.Out.(io.Closer); ok {
			return f.Close()
		}
	}
	return c.Close()
}

// Close free resources for the client. Always call this function when using NewClient
func (c *client) Close() error {
	if c.dataLink != nil {
		c.dataLink.Close()
	}

	return nil
}
