package pipeline

import (
	"sync"
	"testing"
	"time"
)

func TestDedupTracker_AllowsFirstRequest(t *testing.T) {
	d := NewDedupTracker(10 * time.Second)
	fp := Fingerprint("hello", "agent1", "session1")
	if !d.CheckAndTrack(fp) {
		t.Fatal("first request should be allowed")
	}
	d.Release(fp)
}

func TestDedupTracker_RejectsConcurrentDuplicate(t *testing.T) {
	d := NewDedupTracker(10 * time.Second)
	fp := Fingerprint("hello", "agent1", "session1")
	if !d.CheckAndTrack(fp) {
		t.Fatal("first request should be allowed")
	}
	// Second concurrent request with same fingerprint should be rejected.
	if d.CheckAndTrack(fp) {
		t.Fatal("concurrent duplicate should be rejected")
	}
	d.Release(fp)
	// After release, the same fingerprint should be allowed again.
	if !d.CheckAndTrack(fp) {
		t.Fatal("after release, request should be allowed again")
	}
	d.Release(fp)
}

func TestDedupTracker_DifferentFingerprints(t *testing.T) {
	d := NewDedupTracker(10 * time.Second)
	fp1 := Fingerprint("hello", "agent1", "session1")
	fp2 := Fingerprint("world", "agent1", "session1")
	if !d.CheckAndTrack(fp1) {
		t.Fatal("first request should be allowed")
	}
	if !d.CheckAndTrack(fp2) {
		t.Fatal("different fingerprint should be allowed concurrently")
	}
	d.Release(fp1)
	d.Release(fp2)
}

func TestDedupTracker_TTLExpiry(t *testing.T) {
	d := NewDedupTracker(50 * time.Millisecond)
	fp := Fingerprint("hello", "agent1", "session1")
	if !d.CheckAndTrack(fp) {
		t.Fatal("first request should be allowed")
	}
	// Don't release — let it expire.
	time.Sleep(100 * time.Millisecond)
	// After TTL, the stale entry should be evicted.
	if !d.CheckAndTrack(fp) {
		t.Fatal("expired entry should be evicted, allowing new request")
	}
	d.Release(fp)
}

func TestDedupTracker_ConcurrentAccess(t *testing.T) {
	d := NewDedupTracker(10 * time.Second)
	fp := Fingerprint("concurrent", "agent1", "session1")

	const goroutines = 100
	allowed := int32(0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if d.CheckAndTrack(fp) {
				mu.Lock()
				allowed++
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				d.Release(fp)
			}
		}()
	}
	wg.Wait()
	// Exactly one should have been allowed at a time (though multiple may
	// succeed sequentially after releases). At minimum, the first must succeed.
	if allowed == 0 {
		t.Fatal("at least one goroutine should have been allowed")
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	fp1 := Fingerprint("hello", "agent1", "session1")
	fp2 := Fingerprint("hello", "agent1", "session1")
	if fp1 != fp2 {
		t.Fatalf("same inputs should produce same fingerprint: %s != %s", fp1, fp2)
	}
}

func TestFingerprint_DifferentContent(t *testing.T) {
	fp1 := Fingerprint("hello", "agent1", "session1")
	fp2 := Fingerprint("world", "agent1", "session1")
	if fp1 == fp2 {
		t.Fatal("different content should produce different fingerprints")
	}
}

func TestDedupTracker_InFlightCount(t *testing.T) {
	d := NewDedupTracker(10 * time.Second)
	if d.InFlightCount() != 0 {
		t.Fatal("empty tracker should have 0 in-flight")
	}
	fp := Fingerprint("hello", "agent1", "session1")
	d.CheckAndTrack(fp)
	if d.InFlightCount() != 1 {
		t.Fatal("should have 1 in-flight")
	}
	d.Release(fp)
	if d.InFlightCount() != 0 {
		t.Fatal("after release should have 0 in-flight")
	}
}
