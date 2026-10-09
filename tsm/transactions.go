package tsm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

var (
	errNotReceiving    = errors.New("bacnet transaction is not receiving")
	errSourceMismatch  = errors.New("bacnet reply source mismatch")
	errServiceMismatch = errors.New("bacnet reply service mismatch")
	errAlreadyAnswered = errors.New("bacnet reply already delivered")
)

// MaxTransaction is the default max number of transactions that can occur
// concurrently
const MaxTransaction = 255
const invalidID = 0

const (
	idle = iota
)

type state struct {
	id           int
	state        int
	requestTimer int
	data         chan interface{}
	source       *btypes.Address
	service      btypes.ServiceConfirmed
}

// TSM is the transaction state manager. It handles passing data to other
// processes and keeping track of what transactions are currently processed
type TSM struct {
	mutex  sync.Mutex
	states map[int]*state
	free   struct {
		id    chan int
		space chan struct{}
	}
}

// New creates a new transaction manager
func New(size int) *TSM {
	t := &TSM{
		states: make(map[int]*state),
	}

	// Generate free ids.
	t.free.id = make(chan int, MaxTransaction)
	for i := invalidID + 1; i < MaxTransaction; i++ {
		t.free.id <- i
	}

	// Generate free space
	t.free.space = make(chan struct{}, size)
	for i := 0; i < size; i++ {
		t.free.space <- struct{}{}
	}

	return t
}

// Send delivers b to the transaction id. src must match the peer recorded by ID.
// A non-nil service must match the confirmed-service choice recorded by ID.
// Nil service skips that check so Reject and Abort, which carry no service
// choice, can still complete the transaction. A mismatch or a second reply
// leaves the transaction pending.
func (t *TSM) Send(src *btypes.Address, id int, service *btypes.ServiceConfirmed, b interface{}) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	s, ok := t.states[id]
	if !ok {
		return errNotReceiving
	}
	if s.source == nil || !addressMatches(s.source, src) {
		return errSourceMismatch
	}
	if service != nil && *service != s.service {
		return errServiceMismatch
	}
	select {
	case s.data <- b:
		return nil
	default:
		return errAlreadyAnswered
	}
}

// Receive attempts to receive a byte array from the invoked id. If a time out
// period has passed then an error is returned
func (t *TSM) Receive(id int, timeout time.Duration) (interface{}, error) {
	t.mutex.Lock()
	s, ok := t.states[id]
	t.mutex.Unlock()

	if !ok {
		return nil, errNotReceiving
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Wait for data
	select {
	case b, ok := <-s.data:
		if !ok {
			return nil, errNotReceiving
		}
		return b, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("receive timed out (%v)", timeout)
	}

}

// ID reserves an invoke id and records the peer and confirmed-service choice
// the reply must match. src is the address the request is sent to.
func (t *TSM) ID(ctx context.Context, src *btypes.Address, service btypes.ServiceConfirmed) (int, error) {
	var id int
	select {
	case <-t.free.space:
		// got a free spot, lets try and get a free id
		select {
		case id = <-t.free.id:
		case err := <-ctx.Done():
			t.free.space <- struct{}{}
			return 0, fmt.Errorf("unable to get a free id: %v", err)
		}
	case err := <-ctx.Done():
		return 0, fmt.Errorf("no free space: %v", err)
	}

	s := &state{
		state:   idle,
		data:    make(chan interface{}, 1),
		source:  copyAddress(src),
		service: service,
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.states[id] = s
	return id, nil
}

// Put allows the id to be reused in the transaction manager
func (t *TSM) Put(id int) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	s, ok := t.states[id]
	if !ok {
		return fmt.Errorf("id %d does not exist in the transactions", id)
	}

	close(s.data)
	t.free.id <- id
	t.free.space <- struct{}{}
	delete(t.states, id)
	return nil
}

func copyAddress(src *btypes.Address) *btypes.Address {
	if src == nil {
		return nil
	}
	out := *src
	if src.Mac != nil {
		out.Mac = append([]uint8(nil), src.Mac...)
	}
	if src.Adr != nil {
		out.Adr = append([]uint8(nil), src.Adr...)
	}
	return &out
}

func addressMatches(expected, actual *btypes.Address) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}
	if expected.Net != actual.Net || expected.Len != actual.Len || expected.MacLen != actual.MacLen {
		return false
	}
	if len(expected.Mac) != len(actual.Mac) || len(expected.Adr) != len(actual.Adr) {
		return false
	}
	for i := range expected.Mac {
		if expected.Mac[i] != actual.Mac[i] {
			return false
		}
	}
	for i := range expected.Adr {
		if expected.Adr[i] != actual.Adr[i] {
			return false
		}
	}
	return true
}
