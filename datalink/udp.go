package datalink

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	pcap "github.com/packetcap/go-pcap"
)

// DefaultPort that BacnetIP will use if a port is not given. Valid ports for
// the bacnet protocol is between 0xBAC0 and 0xBAC9
const DefaultPort = 0xBAC0 //47808

// errNotForSocket is returned when pcap sees a datagram for another socket.
// ClientRun skips it. It is not logged.
var errNotForSocket = errors.New("udp datagram is not for this socket")

type udpDataLink struct {
	netInterface                *net.Interface
	myAddress, broadcastAddress *btypes.Address
	port                        int
	listener                    *net.UDPConn
}

type pcapDataLink struct {
	udpDataLink
	pcapHandle    *pcap.Handle
	interfaceName string
	// localIP/localPort is the ephemeral send socket. myAddress keeps the
	// configured BACnet port for callers that want that port.
	localIP     net.IP
	localPort   int
	bacnetPort  int
	broadcastIP net.IP
}

/*
NewUDPDataLink returns udp listener
pass in your iface port by name, see an alternative NewUDPDataLinkFromIP if you wish to pass in by ip and subnet
  - inter: eth0
  - addr: 47808
*/
func NewUDPDataLink(inter string, port int) (link DataLink, err error) {
	if port == 0 {
		port = DefaultPort
	}
	addr := inter
	if !strings.ContainsRune(inter, '/') {
		addr, err = FindCIDRAddress(inter)
		if err != nil {
			return nil, err
		}
	}
	link, err = dataLink(addr, port)
	if err != nil {
		return nil, err
	}
	return link, nil
}

func NewPcapDataLink(inter string, port int, timeout time.Duration) (link DataLink, err error) {
	if port == 0 {
		port = DefaultPort
	}

	if timeout == 0 {
		timeout = time.Second * 60
	}

	// Open pcap handle for the interface
	handle, err := pcap.OpenLive(context.Background(), inter, 1600, true, timeout, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap handle: %w", err)
	}

	// Get interface address for myAddress
	addr, err := FindCIDRAddress(inter)
	if err != nil {
		handle.Close()
		return nil, err
	}

	// Port 0 asks the kernel for an ephemeral source port. Confirmed replies
	// are unicast back to that port. `port` is the BACnet UDP port devices
	// send from and broadcasts use (usually 47808), not this socket.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		handle.Close()
		return nil, fmt.Errorf("failed to create UDP socket: %w", err)
	}
	udpLocal, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udpLocal.Port == 0 {
		handle.Close()
		conn.Close()
		return nil, fmt.Errorf("pcap socket has no local port")
	}

	// Parse IP and create addresses
	ip, ipNet, err := net.ParseCIDR(addr)
	if err != nil {
		handle.Close()
		conn.Close()
		return nil, err
	}

	broadcast := net.IP(make([]byte, 4))
	for i := range broadcast {
		broadcast[i] = ipNet.IP[i] | ^ipNet.Mask[i]
	}

	// Keep the filter flat: go-pcap rejects host/broadcast qualifiers and mis-evaluates "ip broadcast".
	// Receive drops unicast to the BACnet port; only this socket and broadcasts stay.
	err = handle.SetBPFFilter(pcapCaptureFilter(udpLocal.Port, port))
	if err != nil {
		handle.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to set BPF filter: %w", err)
	}

	return &pcapDataLink{
		udpDataLink: udpDataLink{
			listener:         conn,
			myAddress:        IPPortToAddress(ip, port),
			broadcastAddress: IPPortToAddress(broadcast, port),
		},
		pcapHandle:    handle,
		interfaceName: inter,
		localIP:       ip.To4(),
		localPort:     udpLocal.Port,
		bacnetPort:    port,
		broadcastIP:   broadcast.To4(),
	}, nil
}

// pcapCaptureFilter stays flat: udp and (dst port <local> or dst port <bacnet>).
// go-pcap rejects host/broadcast qualifiers and mis-evaluates "ip broadcast".
func pcapCaptureFilter(localPort, bacnetPort int) string {
	return fmt.Sprintf("udp and (dst port %d or dst port %d)", localPort, bacnetPort)
}

// acceptCapturedUDP keeps unicast to this socket, and broadcasts on the BACnet port.
func acceptCapturedUDP(dstIP net.IP, dstPort, localPort, bacnetPort int, localIP, broadcastIP net.IP) bool {
	ip := dstIP.To4()
	if ip == nil {
		return false
	}
	if dstPort == localPort && ip.Equal(localIP) {
		return true
	}
	return dstPort == bacnetPort && (ip.Equal(broadcastIP) || ip.Equal(net.IPv4bcast))
}

/*
NewUDPDataLinkFromIP returns udp listener
  - addr: 192.168.15.10
  - subNet: 24
  - addr: 47808
*/
func NewUDPDataLinkFromIP(addr string, subNet, port int) (link DataLink, err error) {
	addr = fmt.Sprintf("%s/%d", addr, subNet)
	link, err = dataLink(addr, port)
	if err != nil {
		return nil, err
	}
	return link, nil
}

func dataLink(ipAddr string, port int) (DataLink, error) {
	if port == 0 {
		port = DefaultPort
	}

	ip, ipNet, err := net.ParseCIDR(ipAddr)
	if err != nil {
		return nil, err
	}

	broadcast := net.IP(make([]byte, 4))
	for i := range broadcast {
		broadcast[i] = ipNet.IP[i] | ^ipNet.Mask[i]
	}

	udp, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", port))
	conn, err := net.ListenUDP("udp", udp)
	if err != nil {
		return nil, err
	}

	return &udpDataLink{
		listener:         conn,
		myAddress:        IPPortToAddress(ip, port),
		broadcastAddress: IPPortToAddress(broadcast, port),
	}, nil
}

func (c *udpDataLink) Close() error {
	if c.listener != nil {
		return c.listener.Close()
	}
	return nil
}

func (c *udpDataLink) Receive(data []byte) (*btypes.Address, int, error) {
	n, adr, err := c.listener.ReadFromUDP(data)
	if err != nil {
		return nil, n, err
	}
	adr.IP = adr.IP.To4()
	udpAddr := UDPToAddress(adr)
	return udpAddr, n, nil
}

func (c *udpDataLink) GetMyAddress() *btypes.Address {
	return c.myAddress
}

// GetBroadcastAddress uses the given address with subnet to return the broadcast address
func (c *udpDataLink) GetBroadcastAddress() *btypes.Address {
	return c.broadcastAddress
}

func (c *udpDataLink) Send(data []byte, npdu *btypes.NPDU, dest *btypes.Address) (int, error) {
	// Get IP Address
	d, err := dest.UDPAddr()
	if err != nil {
		return 0, err
	}
	return c.listener.WriteTo(data, &d)
}

func (c *pcapDataLink) Close() error {
	var errs []error

	if c.pcapHandle != nil {
		c.pcapHandle.Close()
	}

	if c.listener != nil {
		if err := c.listener.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing pcapDataLink: %v", errs)
	}
	return nil
}

func (c *pcapDataLink) Receive(data []byte) (*btypes.Address, int, error) {
	src, dstIP, dstPort, n, err := c.readPacket(data)
	if err != nil {
		return nil, n, err
	}
	if !acceptCapturedUDP(dstIP, dstPort, c.localPort, c.bacnetPort, c.localIP, c.broadcastIP) {
		return nil, 0, errNotForSocket
	}
	return src, n, nil
}

func (c *pcapDataLink) readPacket(data []byte) (*btypes.Address, net.IP, int, int, error) {
	// Capture packet using pcap
	packetData, _, err := c.pcapHandle.ReadPacketData()
	if err != nil {
		return nil, nil, 0, 0, err
	}

	parsedPacket := gopacket.NewPacket(packetData, layers.LayerTypeEthernet, gopacket.NoCopy)

	ipLayer := parsedPacket.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		return nil, nil, 0, 0, fmt.Errorf("no ip layer found")
	}

	udpLayer := parsedPacket.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return nil, nil, 0, 0, fmt.Errorf("no udp layer found")
	}

	// Copy packet data to the provided buffer
	n := copy(data, udpLayer.LayerPayload())

	// Capture the address from the ip layer
	ip, ok := ipLayer.(*layers.IPv4)
	if !ok {
		return nil, nil, 0, 0, fmt.Errorf("ip layer is not a ipv4 layer")
	}
	udp, ok := udpLayer.(*layers.UDP)
	if !ok {
		return nil, nil, 0, 0, fmt.Errorf("udp layer is not a udp layer")
	}

	srcIP := ip.SrcIP.To4()
	dstIP := ip.DstIP.To4()
	if srcIP == nil || dstIP == nil {
		return nil, nil, 0, 0, fmt.Errorf("ip layer is not ipv4")
	}

	src := UDPToAddress(&net.UDPAddr{IP: srcIP, Port: int(udp.SrcPort)})
	return src, dstIP, int(udp.DstPort), n, nil
}

// IPPortToAddress converts a given udp address into a bacnet address
func IPPortToAddress(ip net.IP, port int) *btypes.Address {
	return UDPToAddress(&net.UDPAddr{
		IP:   ip.To4(),
		Port: port,
	})
}

// UDPToAddress converts a given udp address into a bacnet address
func UDPToAddress(n *net.UDPAddr) *btypes.Address {
	a := &btypes.Address{}
	p := uint16(n.Port)
	// Length of IP plus the port
	length := net.IPv4len + 2
	a.Mac = make([]uint8, length)
	//Encode ip
	for i := 0; i < net.IPv4len; i++ {
		a.Mac[i] = n.IP[i]
	}
	// Encode port
	a.Mac[net.IPv4len+0] = uint8(p >> 8)
	a.Mac[net.IPv4len+1] = uint8(p & 0x00FF)

	a.MacLen = uint8(length)
	return a
}

// FindCIDRAddress find out CIDR address from net interface
func FindCIDRAddress(inter string) (string, error) {
	i, err := net.InterfaceByName(inter)
	if err != nil {
		return "", err
	}

	uni, err := i.Addrs()
	if err != nil {
		return "", err
	}

	if len(uni) == 0 {
		return "", fmt.Errorf("interface %s has no addresses", inter)
	}

	// Find the first IP4 ip
	for _, adr := range uni {
		IP, _, _ := net.ParseCIDR(adr.String())

		// To4 is non nil when the type is ip4
		if IP.To4() != nil {
			return adr.String(), nil
		}
	}
	// We couldn't find a interface or all of them are ip6
	return "", fmt.Errorf("no valid broadcasting address was found on interface %s", inter)
}
