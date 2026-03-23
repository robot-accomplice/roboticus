package core

import (
	"testing"
	"time"
)

func TestOrDone_NormalCompletion(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	input := make(chan int, 3)
	input <- 1
	input <- 2
	input <- 3
	close(input)

	var results []int
	for v := range OrDone(done, input) {
		results = append(results, v)
	}

	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestOrDone_DoneCancellation(t *testing.T) {
	done := make(chan struct{})
	input := make(chan int) // unbuffered, will block

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	}()

	count := 0
	for range OrDone(done, input) {
		count++
	}

	if count != 0 {
		t.Errorf("should receive 0 values when done closes, got %d", count)
	}
}

func TestOrDoneFunc_NormalCompletion(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	err := OrDoneFunc(done, func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestOrDoneFunc_DoneCancellation(t *testing.T) {
	done := make(chan struct{})

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	}()

	err := OrDoneFunc(done, func() error {
		time.Sleep(1 * time.Second) // would block, but done closes first
		return nil
	})

	if err != nil {
		t.Errorf("expected nil when done closes first, got %v", err)
	}
}
