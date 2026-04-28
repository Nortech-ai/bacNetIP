package tsm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTSM(t *testing.T) {
	size := 3
	tsm := New(size)
	ctx := t.Context()
	var err error
	for range size - 1 {
		_, err = tsm.ID(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}

	id, err := tsm.ID(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The buffer should be full at this point.
	ctx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	_, err = tsm.ID(ctx)
	if err == nil {
		t.Fatal("Buffer was full but an id was given ")
	}

	// Free an ID
	err = tsm.Put(id)
	if err != nil {
		t.Fatal(err)
	}

	// Now we should be able to get a new id since we free id
	_, err = tsm.ID(t.Context())
	if err != nil {
		t.Fatal(err)
	}

}

func TestDataTransaction(t *testing.T) {
	size := 2
	tsm := New(size)
	ids := make([]int, size)

	for i := range size {
		id, err := tsm.ID(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}

	expected := map[int]string{
		ids[0]: "Hello First ID",
		ids[1]: "Hello Second ID",
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(ids)*2)
	recvCh := make(chan string, len(ids))

	for _, id := range ids {
		msg := expected[id]

		wg.Go(func() {
			if sendErr := tsm.Send(id, msg); sendErr != nil {
				errCh <- fmt.Errorf("send %d: %w", id, sendErr)
			}
		})

		wg.Go(func() {
			b, recvErr := tsm.Receive(id, 5*time.Second)
			if recvErr != nil {
				errCh <- fmt.Errorf("receive %d: %w", id, recvErr)
				return
			}

			s, ok := b.(string)
			if !ok {
				errCh <- fmt.Errorf("type was not preserved for id %d", id)
				return
			}
			recvCh <- s
		})
	}

	wg.Wait()
	close(errCh)
	close(recvCh)

	for err := range errCh {
		t.Fatal(err)
	}

	received := make(map[string]int, len(ids))
	for msg := range recvCh {
		received[msg]++
	}

	for _, msg := range expected {
		if received[msg] != 1 {
			t.Fatalf("expected message %q once, got %d", msg, received[msg])
		}
	}

	if len(received) != len(expected) {
		t.Fatalf("expected %d unique messages, got %d", len(expected), len(received))
	}
}
