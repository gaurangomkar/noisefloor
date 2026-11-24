// Package webhook receives Alertmanager notifications and stores them.
package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/source/amjson"
	"github.com/gaurangomkar/noisefloor/internal/source/sqlite"
)

// Server is an HTTP listener for Alertmanager and for acknowledgement posts.
type Server struct {
	Store  *sqlite.Store
	Logger *slog.Logger
	Token  string
	Now    func() time.Time
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /{$}", s.help)
	mux.HandleFunc("POST /webhook", s.webhook)
	mux.HandleFunc("POST /ack", s.ack)
	return mux
}

// ListenAndServe serves until ctx is cancelled, then shuts down.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		<-errc
		return err
	}
}

func (s *Server) help(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "noisefloor webhook\nPOST /webhook\nPOST /ack\nGET /healthz\n")
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, amjson.MaxBytes+1))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if len(body) > amjson.MaxBytes {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	events, err := amjson.Parse(body, s.now())
	if err != nil {
		s.log().Warn("webhook rejected", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for i := range events {
		if events[i].Source == "" || events[i].Source == "webhook" {
			events[i].Source = "webhook"
		}
	}
	if err := s.Store.Upsert(r.Context(), events); err != nil {
		s.log().Error("webhook store", "err", err)
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	s.log().Info("stored alerts", "count", len(events))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]int{"stored": len(events)})
}

type ackRequest struct {
	Fingerprint string `json:"fingerprint"`
	StartsAt    string `json:"starts_at"`
	AckedAt     string `json:"acked_at"`
}

func (s *Server) ack(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req ackRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	var starts time.Time
	if req.StartsAt != "" {
		var err error
		starts, err = time.Parse(time.RFC3339, req.StartsAt)
		if err != nil {
			http.Error(w, "invalid starts_at", http.StatusBadRequest)
			return
		}
	}
	acked := s.now()
	if req.AckedAt != "" {
		var err error
		acked, err = time.Parse(time.RFC3339, req.AckedAt)
		if err != nil {
			http.Error(w, "invalid acked_at", http.StatusBadRequest)
			return
		}
	}
	err := s.Store.Ack(r.Context(), req.Fingerprint, starts, acked)
	if errors.Is(err, sqlite.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log().Error("ack", "err", err)
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	got := r.Header.Get("X-Noisefloor-Token")
	if got == "" {
		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		if len(h) > len(prefix) && h[:len(prefix)] == prefix {
			got = h[len(prefix):]
		}
	}
	if len(got) != len(s.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Compile-time check that stored events stay in the model the rest of the tool uses.
var _ = model.Event{}
