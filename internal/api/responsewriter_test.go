package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

	rw.WriteHeader(http.StatusCreated)
	if rw.status != http.StatusCreated {
		t.Errorf("status = %d, want 201", rw.status)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("underlying status = %d, want 201", rec.Code)
	}
}

func TestResponseWriter_DefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

	if rw.status != http.StatusOK {
		t.Errorf("default status = %d, want 200", rw.status)
	}
}

func TestResponseWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

	// httptest.ResponseRecorder implements http.Flusher, so this should work.
	rw.Flush()
	if !rec.Flushed {
		t.Error("Flush should propagate to underlying ResponseWriter")
	}
}

func TestResponseWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

	unwrapped := rw.Unwrap()
	if unwrapped != rec {
		t.Error("Unwrap should return the underlying ResponseWriter")
	}
}

func TestResponseWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

	n, err := rw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// nonFlushWriter is a ResponseWriter that does NOT implement http.Flusher.
type nonFlushWriter struct {
	http.ResponseWriter
}

func TestResponseWriter_FlushNonFlusher(t *testing.T) {
	// When the underlying writer does not implement http.Flusher,
	// Flush should be a no-op (not panic).
	rw := &responseWriter{ResponseWriter: &nonFlushWriter{httptest.NewRecorder()}, status: http.StatusOK}
	rw.Flush() // Should not panic.
}
