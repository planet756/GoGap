package eastmoney

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestNewClientUsesAKShareAlignedDefaultTimeout(t *testing.T) {
	client := NewClient(Options{})
	if client.httpClient.Timeout != 15*time.Second {
		t.Fatalf("default EastMoney timeout = %v, want %v", client.httpClient.Timeout, 15*time.Second)
	}
	if client.minInterval != akShareRequestDelayMin {
		t.Fatalf("default EastMoney request interval = %v, want AKShare min delay %v", client.minInterval, akShareRequestDelayMin)
	}
}

func TestDefaultRetryBackoffDelayMatchesAKShareBounds(t *testing.T) {
	delays := map[time.Duration]bool{}
	for range 64 {
		delay := retryBackoffDelay(1)
		if delay < 2500*time.Millisecond || delay > 3500*time.Millisecond {
			t.Fatalf("retryBackoffDelay(1) = %v, want AKShare second retry delay within [2.5s, 3.5s]", delay)
		}
		delays[delay] = true
	}
	if len(delays) == 1 {
		t.Fatalf("expected AKShare-style jittered retry backoff, got one fixed delay")
	}
}

func TestClientRetriesThreeTimesForTransientErrors(t *testing.T) {
	attempts := 0
	oldDelay := retryBackoffDelay
	retryBackoffDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { retryBackoffDelay = oldDelay })

	client := NewClient(Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, io.ErrUnexpectedEOF
	})}})

	_, _, err := client.getWithRetry(context.Background(), "https://example.test/api")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected final transient error, got %v", err)
	}
	if attempts != akShareMaxRetries {
		t.Fatalf("expected %d AKShare-aligned attempts, got %d", akShareMaxRetries, attempts)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
