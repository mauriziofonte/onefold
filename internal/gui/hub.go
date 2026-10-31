// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"fmt"
	"sync"
)

// hub fans server-sent events out to every open page. A slow page drops
// events instead of blocking the engine (the next job snapshot repairs it).
type hub struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
	closed  bool
}

func newHub() *hub { return &hub{clients: map[chan []byte]bool{}} }

func (h *hub) subscribe() chan []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 512)
	if h.closed {
		close(ch)
		return ch
	}
	h.clients[ch] = true
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[ch] {
		delete(h.clients, ch)
		close(ch)
	}
}

func (h *hub) send(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}
