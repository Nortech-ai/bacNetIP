package tsm

import (
	"context"
	"testing"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

func TestTSM(t *testing.T) {
	size := 3
	tsm := New(size)
	ctx := context.Background()
	var err error
	for i := 0; i < size-1; i++ {
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
	_, err = tsm.ID(context.Background())
	if err != nil {
		t.Fatal(err)
	}

}

func TestDataTransaction(t *testing.T) {
	size := 2
	tsm := New(size)
	ids := make([]int, size)
	var err error

	for i := 0; i < size; i++ {
		ids[i], err = tsm.ID(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}

	go func() {
		err = tsm.Send(ids[0], "Hello First ID")
		if err != nil {
			t.Error(err)
		}
	}()

	go func() {
		err = tsm.Send(ids[1], "Hello Second ID")
		if err != nil {
			t.Error(err)
		}
	}()

	go func() {
		b, err := tsm.Receive(ids[0], time.Duration(5)*time.Second)
		if err != nil {
			t.Error(err)
		}
		s, ok := b.(string)
		if !ok {
			t.Errorf("type was not preseved")
			return
		}
		t.Log(s)
	}()

	b, err := tsm.Receive(ids[1], time.Duration(5)*time.Second)
	if err != nil {
		t.Error(err)
	}

	s, ok := b.(string)
	if !ok {
		t.Errorf("type was not preseved")
		return
	}
	t.Log(s)
}

func TestSourceCorrelation(t *testing.T) {
	tsm := New(1)
	id, err := tsm.ID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tsm.Put(id)

	expected := &btypes.Address{
		Net:    1,
		Len:    1,
		MacLen: 1,
		Mac:    []uint8{1},
		Adr:    []uint8{1},
	}
	if err := tsm.ExpectSource(id, expected); err != nil {
		t.Fatal(err)
	}

	mismatch := &btypes.Address{
		Net:    2,
		Len:    1,
		MacLen: 1,
		Mac:    []uint8{2},
		Adr:    []uint8{2},
	}
	if err := tsm.SendFrom(mismatch, id, "bad"); err == nil {
		t.Fatal("expected source mismatch error")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := tsm.SendFrom(expected, id, "ok"); err != nil {
			t.Errorf("send with expected source failed: %v", err)
		}
	}()

	got, err := tsm.Receive(id, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("expected ok, got %v", got)
	}
	<-done
}

func TestSourceCorrelationFallback(t *testing.T) {
	tsm := New(1)
	id, err := tsm.ID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tsm.Put(id)

	go func() {
		if sendErr := tsm.Send(id, "fallback"); sendErr != nil {
			t.Errorf("send failed: %v", sendErr)
		}
	}()

	got, err := tsm.Receive(id, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fallback" {
		t.Fatalf("expected fallback, got %v", got)
	}
}
