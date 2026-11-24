package webhook

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/source/sqlite"
)

func TestWebhookPersistsAndAck(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 2, 23, 16, 0, 0, 0, time.UTC)
	srv := &Server{
		Store:  store,
		Token:  "sekret",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
	}
	body := []byte(`{
	  "receiver": "pager",
	  "status": "firing",
	  "alerts": [{
	    "status": "firing",
	    "labels": {"alertname": "InstanceDown"},
	    "startsAt": "2026-02-23T15:00:00Z",
	    "endsAt": "2026-02-23T15:05:00Z",
	    "fingerprint": "abc"
	  }]
	}`)
	unauth := post(srv, "/webhook", body, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth %d", unauth.Code)
	}
	res := post(srv, "/webhook", body, "sekret")
	if res.Code != http.StatusAccepted {
		t.Fatalf("webhook %d %s", res.Code, res.Body.String())
	}
	ackBody := []byte(`{"fingerprint":"abc","starts_at":"2026-02-23T15:00:00Z","acked_at":"2026-02-23T15:04:00Z"}`)
	ack := post(srv, "/ack", ackBody, "Bearer sekret")
	if ack.Code != http.StatusNoContent {
		t.Fatalf("ack %d %s", ack.Code, ack.Body.String())
	}
	got, err := store.Fetch(context.Background(), model.Window{
		Start: now.Add(-24 * time.Hour),
		End:   now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("%+v", got.Events)
	}
	ev := got.Events[0]
	if ev.Status != model.StatusFiring || !ev.AckKnown || ev.Receiver != "pager" {
		t.Fatalf("%+v", ev)
	}
	if !ev.AckedAt.Equal(time.Date(2026, 2, 23, 15, 4, 0, 0, time.UTC)) {
		t.Fatalf("ack time %s", ev.AckedAt)
	}
	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	hw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(hw, health)
	if hw.Code != http.StatusOK {
		t.Fatalf("health %d", hw.Code)
	}
}

func post(srv *Server, path string, body []byte, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if token != "" {
		if len(token) > 7 && token[:7] == "Bearer " {
			req.Header.Set("Authorization", token)
		} else {
			req.Header.Set("X-Noisefloor-Token", token)
		}
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}
