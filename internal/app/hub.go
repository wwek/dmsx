package app

import "sync"

type hub struct {
	mu    sync.Mutex
	feeds map[string]*feed
}

type feed struct {
	history [][]byte
	subs    map[chan []byte]struct{}
}

func newHub() *hub { return &hub{feeds: map[string]*feed{}} }

func (h *hub) Publish(id string, payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	item := append([]byte(nil), payload...)
	f := h.feed(id)
	f.history = append(f.history, item)
	if len(f.history) > 200 {
		f.history = f.history[len(f.history)-200:]
	}
	for sub := range f.subs {
		select {
		case sub <- item:
		default:
		}
	}
}

func (h *hub) Subscribe(id string) (<-chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.feed(id)
	ch := make(chan []byte, 200)
	for _, item := range f.history {
		ch <- item
	}
	f.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(f.subs, ch)
	}
}

func (h *hub) feed(id string) *feed {
	f := h.feeds[id]
	if f == nil {
		f = &feed{subs: map[chan []byte]struct{}{}}
		h.feeds[id] = f
	}
	return f
}
