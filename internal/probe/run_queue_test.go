package probe

import (
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
	q.notifyOne()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pop did not return within 2s after push+notify")
	}

	if got != "probe-x" {
		t.Errorf("pop returned %q, want %q", got, "probe-x")
	}
}
