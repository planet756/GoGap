package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"gogap/internal/domain"
)

const (
	DefaultBufferSize        = 8
	DefaultHeartbeatInterval = 15 * time.Second

	EventSnapshot = "snapshot"
)

type Event struct {
	Name string
	Data []byte
}

type Hub struct {
	mu         sync.RWMutex
	subs       map[*Subscription]struct{}
	bufferSize int
}

type Subscription struct {
	hub    *Hub
	events chan Event
	done   chan struct{}
	once   sync.Once
}

func NewHub(bufferSize int) *Hub {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &Hub{
		subs:       map[*Subscription]struct{}{},
		bufferSize: bufferSize,
	}
}

func (h *Hub) Subscribe(ctx context.Context) *Subscription {
	sub := &Subscription{
		hub:    h,
		events: make(chan Event, h.bufferSize),
		done:   make(chan struct{}),
	}

	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			sub.Close()
		case <-sub.done:
		}
	}()

	return sub
}

func (h *Hub) BroadcastSnapshot(snapshot domain.SnapshotResponse) error {
	snapshot = domain.NormalizeSnapshotResponse(snapshot)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("sse: marshal snapshot: %w", err)
	}
	h.broadcast(Event{Name: EventSnapshot, Data: payload})
	return nil
}

func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Close closes all active subscriptions. Safe to call multiple times.
func (h *Hub) Close() {
	h.mu.RLock()
	subs := make([]*Subscription, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.mu.RUnlock()
	for _, sub := range subs {
		sub.Close()
	}
}

func (h *Hub) broadcast(event Event) {
	h.mu.RLock()
	slow := make([]*Subscription, 0)
	for sub := range h.subs {
		select {
		case sub.events <- event:
		default:
			slow = append(slow, sub)
		}
	}
	h.mu.RUnlock()

	for _, sub := range slow {
		sub.Close()
	}
}

func (s *Subscription) Events() <-chan Event {
	return s.events
}

func (s *Subscription) Close() {
	s.once.Do(func() {
		s.hub.mu.Lock()
		if _, ok := s.hub.subs[s]; ok {
			delete(s.hub.subs, s)
			close(s.events)
		}
		close(s.done)
		s.hub.mu.Unlock()
	})
}

type SnapshotProvider interface {
	CurrentSnapshot() domain.SnapshotResponse
}

type Handler struct {
	Hub               *Hub
	SnapshotProvider  SnapshotProvider
	HeartbeatInterval time.Duration
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Hub == nil {
		http.Error(w, "sse hub unavailable", http.StatusServiceUnavailable)
		return
	}

	setHeaders(w.Header())
	flusher, _ := w.(http.Flusher)
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	flush(flusher)

	if h.SnapshotProvider != nil {
		if err := WriteSnapshot(w, h.SnapshotProvider.CurrentSnapshot()); err != nil {
			return
		}
		flush(flusher)
	}

	interval := h.HeartbeatInterval
	if interval <= 0 {
		interval = DefaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sub := h.Hub.Subscribe(r.Context())
	defer sub.Close()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			if err := WriteEvent(w, event); err != nil {
				return
			}
			flush(flusher)
		case <-ticker.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flush(flusher)
		}
	}
}

func WriteSnapshot(w io.Writer, snapshot domain.SnapshotResponse) error {
	snapshot = domain.NormalizeSnapshotResponse(snapshot)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("sse: marshal snapshot: %w", err)
	}
	return WriteEvent(w, Event{Name: EventSnapshot, Data: payload})
}

func WriteEvent(w io.Writer, event Event) error {
	if event.Name != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event.Name); err != nil {
			return err
		}
	}
	if len(event.Data) == 0 {
		_, err := io.WriteString(w, "data: \n\n")
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", event.Data); err != nil {
		return err
	}
	return nil
}

func setHeaders(header http.Header) {
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
}

func flush(flusher http.Flusher) {
	if flusher != nil {
		flusher.Flush()
	}
}
