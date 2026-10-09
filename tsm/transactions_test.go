package tsm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

var testSrc = &btypes.Address{MacLen: 1, Mac: []uint8{1}}

func testID(ctx context.Context, m *TSM, src *btypes.Address, svc btypes.ServiceConfirmed) (int, error) {
	if src == nil {
		src = testSrc
	}
	if svc == 0 {
		svc = btypes.ServiceConfirmedReadProperty
	}
	return m.ID(ctx, src, svc)
}

func TestTSM(t *testing.T) {
	size := 3
	tsm := New(size)
	ctx := context.Background()
	var err error
	for i := 0; i < size-1; i++ {
		_, err = testID(ctx, tsm, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}

	id, err := testID(ctx, tsm, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	_, err = testID(ctx, tsm, nil, 0)
	if err == nil {
		t.Fatal("Buffer was full but an id was given ")
	}

	err = tsm.Put(id)
	if err != nil {
		t.Fatal(err)
	}

	_, err = testID(context.Background(), tsm, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDataTransaction(t *testing.T) {
	size := 2
	m := New(size)
	ids := make([]int, size)
	var err error

	for i := 0; i < size; i++ {
		ids[i], err = testID(context.Background(), m, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}

	go func() {
		if sendErr := m.Send(testSrc, ids[0], nil, "Hello First ID"); sendErr != nil {
			t.Error(sendErr)
		}
	}()

	go func() {
		if sendErr := m.Send(testSrc, ids[1], nil, "Hello Second ID"); sendErr != nil {
			t.Error(sendErr)
		}
	}()

	go func() {
		b, recvErr := m.Receive(ids[0], 5*time.Second)
		if recvErr != nil {
			t.Error(recvErr)
			return
		}
		if _, ok := b.(string); !ok {
			t.Error("type was not preseved")
		}
	}()

	b, err := m.Receive(ids[1], 5*time.Second)
	if err != nil {
		t.Error(err)
	}
	if _, ok := b.(string); !ok {
		t.Error("type was not preseved")
	}
}

func TestSendChecksPeerAndService(t *testing.T) {
	m := New(1)
	expected := &btypes.Address{Net: 2001, Len: 1, MacLen: 6, Mac: []uint8{192, 168, 1, 1, 0xBA, 0xC0}, Adr: []uint8{0x1D}}
	svc := btypes.ServiceConfirmedReadPropMultiple
	id, err := m.ID(t.Context(), expected, svc)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Put(id)

	mismatch := &btypes.Address{Net: 2001, Len: 1, MacLen: 6, Mac: []uint8{10, 0, 0, 1, 0xBA, 0xC0}, Adr: []uint8{0x1D}}
	if err := m.Send(mismatch, id, &svc, "bad"); !errors.Is(err, errSourceMismatch) {
		t.Fatalf("source: got %v", err)
	}
	bare := &btypes.Address{MacLen: 6, Mac: append([]uint8(nil), expected.Mac...)}
	if err := m.Send(bare, id, &svc, "bare"); !errors.Is(err, errSourceMismatch) {
		t.Fatalf("bare udp: got %v", err)
	}
	wrong := btypes.ServiceConfirmedReadProperty
	if err := m.Send(expected, id, &wrong, "svc"); !errors.Is(err, errServiceMismatch) {
		t.Fatalf("service: got %v", err)
	}
	if err := m.Send(expected, id, &svc, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(expected, id, &svc, "again"); !errors.Is(err, errAlreadyAnswered) {
		t.Fatalf("duplicate: got %v", err)
	}
	got, err := m.Receive(id, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("got %v", got)
	}
}

func TestReplyAfterTimeoutIsNotReused(t *testing.T) {
	m := New(1)
	src := testSrc
	svc := btypes.ServiceConfirmedReadProperty
	id, err := m.ID(context.Background(), src, svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Receive(id, 0); err == nil {
		t.Fatal("expected timeout")
	}
	if err := m.Put(id); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(src, id, &svc, "late"); err == nil {
		t.Fatal("late reply accepted")
	}

	var reused int
	for i := 0; i < MaxTransaction; i++ {
		reused, err = m.ID(context.Background(), src, svc)
		if err != nil {
			t.Fatal(err)
		}
		if reused == id {
			break
		}
		if err := m.Put(reused); err != nil {
			t.Fatal(err)
		}
	}
	if reused != id {
		t.Fatal("invoke id was not reused")
	}
	if err := m.Send(src, id, &svc, "stale"); err != nil {
		t.Fatal(err)
	}
	if err := m.Put(id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxTransaction; i++ {
		reused, err = m.ID(context.Background(), src, svc)
		if err != nil {
			t.Fatal(err)
		}
		if reused == id {
			break
		}
		if err := m.Put(reused); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Send(src, id, &svc, "fresh"); err != nil {
		t.Fatalf("stale reply still occupies the transaction: %v", err)
	}
	got, err := m.Receive(id, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fresh" {
		t.Fatalf("got %v", got)
	}
}
