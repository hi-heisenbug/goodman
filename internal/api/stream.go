package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleStream(w http.ResponseWriter, request *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": hb\n\n")
			flusher.Flush()
		case message := <-ch:
			_, _ = w.Write(message)
			flusher.Flush()
		}
	}
}

func (s *Server) broadcast(event string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	message := []byte("event: " + event + "\ndata: " + string(data) + "\n\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	for subscriber := range s.subs {
		select {
		case subscriber <- message:
		default:
		}
	}
}
