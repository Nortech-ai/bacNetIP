package datalink

import (
	"net"
	"testing"

	"github.com/packetcap/go-pcap/filter"
	"golang.org/x/net/bpf"
)

func TestPcapFilterAndDestination(t *testing.T) {
	const (
		localPort  = 49298
		otherPort  = 55236
		bacnetPort = DefaultPort
	)
	localIP := net.IPv4(192, 168, 0, 165).To4()
	broadcastIP := net.IPv4(192, 168, 0, 255).To4()

	expr := pcapCaptureFilter(localPort, bacnetPort)
	vm := compileCaptureFilter(t, expr)

	frames := []struct {
		name    string
		dstIP   net.IP
		dstPort int
		bpfKeep bool
		code    bool
	}{
		{"reply to my port", localIP, localPort, true, true},
		{"reply to other process", localIP, otherPort, false, false},
		{"subnet broadcast", broadcastIP, bacnetPort, true, true},
		{"global broadcast", net.IPv4bcast, bacnetPort, true, true},
		{"unicast to my ip on bacnet port", localIP, bacnetPort, true, false},
	}
	for _, tc := range frames {
		t.Run(tc.name, func(t *testing.T) {
			n, err := vm.Run(udpFrame(tc.dstIP, tc.dstPort))
			if err != nil {
				t.Fatal(err)
			}
			if got := n > 0; got != tc.bpfKeep {
				t.Fatalf("bpf keep=%v want %v", got, tc.bpfKeep)
			}
			got := acceptCapturedUDP(tc.dstIP, tc.dstPort, localPort, bacnetPort, localIP, broadcastIP)
			if got != tc.code {
				t.Fatalf("accept=%v want %v", got, tc.code)
			}
		})
	}
}

func compileCaptureFilter(t *testing.T, expr string) *bpf.VM {
	t.Helper()
	e := filter.NewExpression(expr)
	if e == nil {
		t.Fatalf("go-pcap rejected %q", expr)
	}
	prog, err := e.Compile().Compile()
	if err != nil {
		t.Fatalf("compile %q: %v", expr, err)
	}
	vm, err := bpf.NewVM(prog)
	if err != nil {
		t.Fatal(err)
	}
	return vm
}

// udpFrame is an Ethernet/IPv4/UDP packet. Offsets match go-pcap's compiler.
func udpFrame(dst net.IP, dstPort int) []byte {
	pkt := make([]byte, 42)
	pkt[12], pkt[13] = 0x08, 0x00
	pkt[14] = 0x45
	pkt[17] = 28
	pkt[23] = 17
	copy(pkt[30:34], dst.To4())
	pkt[34], pkt[35] = 0xBA, 0xC0
	pkt[36] = byte(dstPort >> 8)
	pkt[37] = byte(dstPort)
	pkt[39] = 8
	return pkt
}
