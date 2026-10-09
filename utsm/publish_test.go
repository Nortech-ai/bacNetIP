package utsm

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConcurrentPublishDoesNotDeadlock(t *testing.T) {
	const n = 50
	m := NewManager(
		DefaultSubscriberTimeout(time.Second),
		DefaultSubscriberLastReceivedTimeout(200*time.Millisecond),
	)
	registered := make(chan struct{})
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		_, _ = m.Collect(1, 1, func() error {
			close(registered)
			return nil
		})
	}()
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("collector did not register")
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(v int) {
			defer wg.Done()
			m.Publish(1, v)
		}(i)
	}
	published := make(chan struct{})
	go func() {
		wg.Wait()
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("publish burst deadlocked")
	}
	select {
	case <-collectDone:
	case <-time.After(2 * time.Second):
		t.Fatal("collector did not return")
	}
}

func TestBeforeWaitErrorDoesNotBlockPublish(t *testing.T) {
	m := NewManager()
	errStop := errors.New("stop")
	collectDone := make(chan error, 1)
	go func() {
		_, err := m.Collect(1, 1, func() error {
			m.Publish(1, "fill")
			go m.Publish(1, "blocked")
			return errStop
		})
		collectDone <- err
	}()
	select {
	case err := <-collectDone:
		if !errors.Is(err, errStop) {
			t.Fatalf("collect returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector blocked after beforeWait error")
	}

	published := make(chan struct{})
	go func() {
		m.Publish(1, "after")
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("publish blocked after collector exit")
	}
}
