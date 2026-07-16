package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetriableStatus(t *testing.T) {
	for code, want := range map[int]bool{
		http.StatusOK:                  false,
		http.StatusNotFound:            false,
		http.StatusUnauthorized:        false,
		http.StatusForbidden:           false,
		http.StatusBadRequest:          false,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable:  true,
		http.StatusGatewayTimeout:      true,
	} {
		if got := RetriableStatus(code); got != want {
			t.Errorf("RetriableStatus(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestRetryDelayBounds(t *testing.T) {
	minWait, maxWait := time.Second, 5*time.Minute
	// The first retry stays within [minWait/2, minWait].
	for i := 0; i < 50; i++ {
		d := RetryDelay(0, minWait, maxWait)
		if d < minWait/2 || d > minWait {
			t.Fatalf("RetryDelay(0) = %s, want within [%s, %s]", d, minWait/2, minWait)
		}
	}
	// After many attempts the ceiling saturates at maxWait, so the wait lands in
	// [maxWait/2, maxWait] and never exceeds the cap.
	for i := 0; i < 50; i++ {
		d := RetryDelay(40, minWait, maxWait)
		if d < maxWait/2 || d > maxWait {
			t.Fatalf("RetryDelay(40) = %s, want within [%s, %s]", d, maxWait/2, maxWait)
		}
	}
}

func TestRepoInfoRetriesOnServerError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"id":"owner/model","sha":"abc123","siblings":[]}`))
	}))
	defer server.Close()

	client := New(server.URL, "", 5*time.Second)
	client.Retries = 5
	client.RetryMinWait = time.Millisecond
	client.RetryMaxWait = 10 * time.Millisecond
	info, err := client.RepoInfo(context.Background(), RepoTypeModel, "owner/model", "main")
	if err != nil {
		t.Fatalf("RepoInfo did not recover from 503: %v", err)
	}
	if info.SHA != "abc123" {
		t.Fatalf("unexpected SHA %q", info.SHA)
	}
	if n := attempts.Load(); n != 3 {
		t.Fatalf("expected 3 attempts (2 x 503 then success), got %d", n)
	}
}

func TestRepoInfoDoesNotRetryClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL, "", 5*time.Second)
	client.Retries = 5
	client.RetryMinWait = time.Millisecond
	client.RetryMaxWait = 10 * time.Millisecond
	if _, err := client.RepoInfo(context.Background(), RepoTypeModel, "owner/model", "main"); err == nil {
		t.Fatal("expected 404 to fail")
	}
	if n := attempts.Load(); n != 1 {
		t.Fatalf("404 is terminal; expected 1 attempt, got %d", n)
	}
}

func TestRepoInfoUnlimitedRetriesHonorContext(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(server.URL, "", 5*time.Second)
	client.Retries = -1 // unlimited
	client.RetryMinWait = time.Millisecond
	client.RetryMaxWait = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.RepoInfo(ctx, RepoTypeModel, "owner/model", "main")
		done <- err
	}()
	// Let it retry a few times, then cancel; unlimited retries must still stop.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unlimited retries did not stop on context cancellation")
	}
	if attempts.Load() < 2 {
		t.Fatalf("expected several retry attempts before cancel, got %d", attempts.Load())
	}
}
