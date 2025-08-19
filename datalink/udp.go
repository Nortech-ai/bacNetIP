package datalink

import (
	"fmt"
	"net"
	"strings"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

// DefaultPort that BacnetIP will use if a port is not given. Valid ports for
// the bacnet protocol is between 0xBAC0 and 0xBAC9
const DefaultPort = 0xBAC0 //47808

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

func NewPcapDataLink(inter string, port int) (link DataLink, err error) {
	if port == 0 {
		port = DefaultPort
	}

	// Open pcap handle for the interface
	handle, err := pcap.OpenLive(inter, 1600, true, pcap.BlockForever)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap handle: %w", err)
	}

	// Set filter for BACnet traffic with specific source port
	err = handle.SetBPFFilter(fmt.Sprintf("udp src port %d", port))
	if err != nil {
		handle.Close()
		return nil, fmt.Errorf("failed to set BPF filter: %w", err)
	}

	// Get interface address for myAddress
	addr, err := FindCIDRAddress(inter)
	if err != nil {
		handle.Close()
		return nil, err
	}

	// Create UDP socket for sending (without binding to port)
	udpAddr := &net.UDPAddr{Port: 0} // Port 0 means any available port
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		handle.Close()
		return nil, fmt.Errorf("failed to create UDP socket: %w", err)
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

	return &pcapDataLink{
		udpDataLink: udpDataLink{
			listener:         conn,
			myAddress:        IPPortToAddress(ip, port),
			broadcastAddress: IPPortToAddress(broadcast, DefaultPort),
		},
		pcapHandle:    handle,
		interfaceName: inter,
	}, nil
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
		broadcastAddress: IPPortToAddress(broadcast, DefaultPort),
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
	// Capture packet using pcap
	packetData, _, err := c.pcapHandle.ReadPacketData()
	if err != nil {
		return nil, 0, err
	}

	parsedPacket := gopacket.NewPacket(packetData, layers.LayerTypeEthernet, gopacket.NoCopy)
	ipLayer := parsedPacket.Layer(layers.LayerTypeIPv4)
	udpLayer := parsedPacket.Layer(layers.LayerTypeUDP)

	// Copy packet data to the provided buffer
	n := copy(data, udpLayer.LayerPayload())
	srcAddr := &net.UDPAddr{}
	if ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv4)
		srcAddr = &net.UDPAddr{
			IP:   ip.SrcIP,
			Port: int(udpLayer.(*layers.UDP).SrcPort),
		}
		adr := UDPToAddress(srcAddr)
		return adr, n, nil
	}

	err = fmt.Errorf("no ip layer found")
	return nil, 0, err
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
