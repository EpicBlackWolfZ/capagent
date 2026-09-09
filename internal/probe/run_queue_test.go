package probe

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunQueue_CloseIdempotent verifies that calling close() multiple times
// is safe and does not panic on the second invocation.
func TestRunQueue_CloseIdempotent(t *testing.T) {
	t.Parallel()

	q := newRunQueue()
	q.close()

	// Second close must be a no-op.
	q.close()

	// Subsequent pops return false.
	if id, ok := q.pop(); ok {
		t.Errorf("pop after close returned id=%q, ok=true; want ok=false", id)
	}
}

// TestRunQueue_PushAfterCloseIsNoop verifies that enqueuing after the queue
// has been closed is silently discarded rather than panicking.
func TestRunQueue_PushAfterCloseIsNoop(t *testing.T) {
	t.Parallel()

	q := newRunQueue()
	q.close()
	q.push("should-be-discarded")

	if id, ok := q.pop(); ok {
		t.Errorf("pop after close returned id=%q, ok=true; want ok=false", id)
	}
}

// TestRunQueue_PopBlocksUntilItem verifies that pop blocks until an item is
// pushed and then returns it.
func TestRunQueue_PopBlocksUntilItem(t *testing.T) {
	t.Parallel()

	q := newRunQueue()

	done := make(chan struct{})
	var got string
	go func() {
		id, ok := q.pop()
		if !ok {
			close(done)
			return
		}
		got = id
		close(done)
	}()

	q.push("probe-x")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pop did not return within 2s after push")
	}

	if got != "probe-x" {
		t.Errorf("pop returned %q, want %q", got, "probe-x")
	}
}

// TestRunQueue_FIFOOrdering verifies that items are returned in the order
// they were pushed.
func TestRunQueue_FIFOOrdering(t *testing.T) {
	t.Parallel()

	q := newRunQueue()
	for _, id := range []string{"first", "second", "third", "fourth"} {
		q.push(id)
	}

	want := []string{"first", "second", "third", "fourth"}
	for i, w := range want {
		got, ok := q.pop()
		if !ok {
			t.Fatalf("pop[%d] returned ok=false", i)
		}
		if got != w {
			t.Errorf("pop[%d] = %q, want %q", i, got, w)
		}
	}

	q.close()

	if id, ok := q.pop(); ok {
		t.Errorf("pop after close returned id=%q, ok=true; want ok=false", id)
	}
}

// TestRunQueue_CloseWakesAllBlockedConsumers verifies that close() releases
// every consumer blocked in pop(). This is the property the previous
// implementation got wrong (it had a buffered channel of capacity 1).
func TestRunQueue_CloseWakesAllBlockedConsumers(t *testing.T) {
	t.Parallel()

	q := newRunQueue()
	const consumers = 8

	var released atomic.Int32
	var wg sync.WaitGroup
	wg.Add(consumers)
	for i := 0; i < consumers; i++ {
		go func() {
			defer wg.Done()
			if _, ok := q.pop(); !ok {
				released.Add(1)
			}
		}()
	}

	// Give the consumers time to actually block inside pop().
	time.Sleep(50 * time.Millisecond)

	q.close()

	waitGroupWithTimeout(t, &wg, 2*time.Second)

	if got := released.Load(); got != consumers {
		t.Errorf("released = %d, want %d", got, consumers)
	}
}

// TestRunQueue_PushWakesBlockedConsumer verifies that push() wakes at least
// one consumer that is currently blocked in pop().
func TestRunQueue_PushWakesBlockedConsumer(t *testing.T) {
	t.Parallel()

	q := newRunQueue()
	const items = 16

	var consumed atomic.Int32
	var wg sync.WaitGroup
	wg.Add(items)
	for i := 0; i < items; i++ {
		go func() {
			defer wg.Done()
			if _, ok := q.pop(); ok {
				consumed.Add(1)
			}
		}()
	}

	// Give the consumers time to block inside pop().
	time.Sleep(20 * time.Millisecond)

	for i := 0; i < items; i++ {
		q.push("x")
	}

	waitGroupWithTimeout(t, &wg, 2*time.Second)

	if got := consumed.Load(); got != items {
		t.Errorf("consumed = %d, want %d", got, items)
	}
}

// TestRunQueue_ConcurrentProducersConsumers hammers the queue with many
// concurrent producers and consumers to exercise the wakeup protocol under
// load. The test is meaningful under -race.
func TestRunQueue_ConcurrentProducersConsumers(t *testing.T) {
	t.Parallel()

	q := newRunQueue()
	const producers = 8
	const perProducer = 200
	totalItems := producers * perProducer

	var consumed atomic.Int32
	var producerWg sync.WaitGroup
	var consumerWg sync.WaitGroup

	producerWg.Add(producers)
	for p := 0; p < producers; p++ {
		p := p
		go func() {
			defer producerWg.Done()
			for i := 0; i < perProducer; i++ {
				q.push(string(rune('a'+p%26)) + string(rune('A'+i%26)))
			}
		}()
	}

	consumerWg.Add(producers)
	for c := 0; c < producers; c++ {
		go func() {
			defer consumerWg.Done()
			for {
				if _, ok := q.pop(); !ok {
					return
				}
				consumed.Add(1)
			}
		}()
	}

	waitGroupWithTimeout(t, &producerWg, 5*time.Second)
	q.close()

	waitGroupWithTimeout(t, &consumerWg, 5*time.Second)

	if got := consumed.Load(); got != int32(totalItems) {
		t.Errorf("consumed = %d, want %d", got, totalItems)
	}
}

// waitGroupWithTimeout blocks until wg.Wait() returns or the timeout elapses.
func waitGroupWithTimeout(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("wait group did not complete within %v", timeout)
	}
}
