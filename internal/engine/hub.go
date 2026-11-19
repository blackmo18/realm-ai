package engine

import (
	"sync"

	"github.com/blackmo18/realm-ai/internal/store"
)

// Hub fans out new messages to live subscribers of a chat (SSE viewers).
// Bots are never subscribers; they are only invoked by routing.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.Message]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[string]map[chan store.Message]struct{}{}} }

// Subscribe returns a channel of new messages in chat and a cancel func.
func (h *Hub) Subscribe(chat string) (<-chan store.Message, func()) {
	ch := make(chan store.Message, 64)
	h.mu.Lock()
	if h.subs[chat] == nil {
		h.subs[chat] = map[chan store.Message]struct{}{}
	}
	h.subs[chat][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[chat], ch)
			if len(h.subs[chat]) == 0 {
				delete(h.subs, chat)
			}
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Publish delivers m to subscribers without blocking; slow subscribers miss
// messages rather than stalling the engine.
func (h *Hub) Publish(m store.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[m.Chat] {
		select {
		case ch <- m:
		default:
		}
	}
}
