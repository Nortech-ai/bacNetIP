package tsm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
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
}

// TSM is the transaction state manager. It handles passing data to other
// processes and keeping track of what transactions are currently processed
type TSM struct {
	mutex  sync.Mutex
	states map[int]*state
	pool   sync.Pool
	free   struct {
		id    chan int
		space chan struct{}
	}
}

// New creates a new transaction manager
func New(size int) *TSM {
	t := &TSM{
		states: make(map[int]*state), pool: sync.Pool{
			// Operation doesn't include a new channel. We want that done when a get is
			// done since we close all channels when putting into the pool.
			New: func() interface{} {
				s := new(state)
				return s
			},
		},
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

// Send data to invoked id
func (t *TSM) Send(id int, b interface{}) error {
	return t.send(nil, id, b)
}

// SendFrom sends data to invoked id while validating expected source, if set.
func (t *TSM) SendFrom(src *btypes.Address, id int, b interface{}) error {
	return t.send(src, id, b)
}

func (t *TSM) send(src *btypes.Address, id int, b interface{}) error {
	t.mutex.Lock()
	s, ok := t.states[id]
	t.mutex.Unlock()

	if !ok {
		return fmt.Errorf("id %d is not receiving", id)
	}
	if s.source != nil {
		// Correlation guard: ignore/don't deliver payloads from unexpected sources.
		if src == nil {
			return fmt.Errorf("id %d source mismatch: expected source set but got nil", id)
		}
		if !addressMatches(s.source, src) {
			return fmt.Errorf("id %d source mismatch: expected %+v got %+v", id, *s.source, *src)
		}
	}
	s.data <- b
	return nil
}

// ExpectSource configures source correlation for responses delivered to id.
func (t *TSM) ExpectSource(id int, src *btypes.Address) error {
	if src == nil {
		return fmt.Errorf("nil source for id %d", id)
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	s, ok := t.states[id]
	if !ok {
		return fmt.Errorf("id %d does not exist in the transactions", id)
	}
	// Store a defensive copy so later caller mutation does not change correlation.
	srcCopy := *src
	if src.Mac != nil {
		srcCopy.Mac = append([]uint8(nil), src.Mac...)
	}
	if src.Adr != nil {
		srcCopy.Adr = append([]uint8(nil), src.Adr...)
	}
	s.source = &srcCopy
	return nil
}

// Receive attempts to receive a byte array from the invoked id. If a time out
// period has passed then an error is returned
func (t *TSM) Receive(id int, timeout time.Duration) (interface{}, error) {
	t.mutex.Lock()
	s, ok := t.states[id]
	t.mutex.Unlock()

	if !ok {
		return nil, fmt.Errorf("id %d is not sending", id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Wait for data
	select {
	case b := <-s.data:
		return b, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("receive timed out (%v)", timeout)
	}

}

// ID returns the invoke id that was used to save the state of this connection.
func (t *TSM) ID(ctx context.Context) (int, error) {
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

	// skip error checking, since we control new generation and what is put in the pool.
	s := t.pool.Get().(*state)
	s.state = idle
	s.requestTimer = 0 // TODO: apdu_timeout
	s.data = make(chan interface{})
	s.source = nil

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
	t.pool.Put(s)
	t.free.id <- id
	t.free.space <- struct{}{}
	delete(t.states, id)
	return nil
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
