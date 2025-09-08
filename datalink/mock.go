package datalink

import (
	"fmt"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

type SentMessage struct {
	Data []byte
	NPDU *btypes.NPDU
	Dest *btypes.Address
}

type ReceivedMessage struct {
	Data []byte
	Src  *btypes.Address
}

type MockDataLink struct {
	// Messages that have been sent with the Send method
	Sent []SentMessage

	// Messages that we are simulating receiving with the Receive method
	Received []ReceivedMessage

	MyAddress        *btypes.Address
	BroadcastAddress *btypes.Address
}

func NewMockDataLink() *MockDataLink {
	return &MockDataLink{
		MyAddress: &btypes.Address{
			Net: 1,
			Adr: []uint8{1, 2, 3, 4},
			Len: 4,
		},
		BroadcastAddress: &btypes.Address{
			Net: 1,
			Adr: []uint8{8, 8, 8, 8},
			Len: 4,
		},
	}
}

func (m *MockDataLink) GetMyAddress() *btypes.Address {
	return m.MyAddress
}

func (m *MockDataLink) GetBroadcastAddress() *btypes.Address {
	return m.BroadcastAddress
}

func (m *MockDataLink) Send(data []byte, npdu *btypes.NPDU, dest *btypes.Address) (int, error) {
	m.Sent = append(m.Sent, SentMessage{Data: data, NPDU: npdu, Dest: dest})
	return len(data), nil
}

func (m *MockDataLink) Receive(data []byte) (*btypes.Address, int, error) {
	if len(m.Received) == 0 {
		return nil, 0, fmt.Errorf("no received messages")
	}
	received := m.Received[0]
	m.Received = m.Received[1:]

	copy(data, received.Data)
	return received.Src, len(received.Data), nil
}

func (m *MockDataLink) Close() error {
	return nil
}
