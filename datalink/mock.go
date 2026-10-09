package datalink

import (
	"io"
	"sync"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

type SentMessage struct {
	Data []byte
	NPDU *btypes.NPDU
	Dest *btypes.Address
}

type ReceivedMessage struct {
	Data  []byte
	Src   *btypes.Address
	Error error
}

type MockDataLink struct {
	// Messages that have been sent with the Send method
	Sent []SentMessage

	// Received is delivered by Receive immediately. Inject holds replies until Send.
	Received []ReceivedMessage

	MyAddress        *btypes.Address
	BroadcastAddress *btypes.Address

	pending []ReceivedMessage
	closed  bool
	mu      sync.Mutex
	cond    *sync.Cond
}

func NewMockDataLink() *MockDataLink {
	m := &MockDataLink{
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
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *MockDataLink) GetMyAddress() *btypes.Address {
	return m.MyAddress
}

func (m *MockDataLink) GetBroadcastAddress() *btypes.Address {
	return m.BroadcastAddress
}

func (m *MockDataLink) Send(data []byte, npdu *btypes.NPDU, dest *btypes.Address) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, SentMessage{Data: data, NPDU: npdu, Dest: dest})
	if len(m.pending) > 0 {
		m.Received = append(m.Received, m.pending...)
		m.pending = nil
		m.cond.Broadcast()
	}
	return len(data), nil
}

func (m *MockDataLink) Receive(data []byte) (*btypes.Address, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.Received) == 0 && !m.closed {
		m.cond.Wait()
	}
	if len(m.Received) == 0 {
		return nil, 0, io.EOF
	}
	received := m.Received[0]
	m.Received = m.Received[1:]
	n := copy(data, received.Data)
	return received.Src, n, received.Error
}

// Inject queues replies. They are delivered on the next Send, after the request exists.
func (m *MockDataLink) Inject(msgs ...ReceivedMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = append(m.pending, msgs...)
}

func (m *MockDataLink) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.cond.Broadcast()
	return nil
}
