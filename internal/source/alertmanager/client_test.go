package alertmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestFetchActiveAndSilenced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			t.Errorf("path %s", r.URL.Path)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "u" || pass != "p" {
			t.Errorf("basic auth %q %q %v", user, pass, ok)
		}
		q := r.URL.Query()
		if q.Get("silenced") != "true" || q.Get("inhibited") != "false" || q.Get("active") != "true" {
			t.Errorf("query %s", q.Encode())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
		  {
		    "labels": {"alertname": "InstanceDown", "severity": "page"},
		    "annotations": {"summary": "down"},
		    "startsAt": "2026-02-23T15:00:00.000Z",
		    "endsAt": "2026-02-23T15:05:00.000Z",
		    "fingerprint": "abc",
		    "status": {"state": "suppressed", "silencedBy": ["abc123"], "inhibitedBy": []}
		  },
		  {
		    "labels": {"alertname": "Old"},
		    "startsAt": "2025-01-01T00:00:00Z",
		    "endsAt": "2025-01-01T01:00:00Z",
		    "status": {"state": "resolved"}
		  }
		]`))
	}))
	defer srv.Close()

	c := Client{
		BaseURL:          srv.URL,
		Username:         "u",
		Password:         "p",
		IncludeSilenced:  true,
		IncludeInhibited: false,
		Now: func() time.Time {
			return time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
		},
	}
	res, err := c.Fetch(context.Background(), model.Window{
		Start: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("events %+v", res.Events)
	}
	ev := res.Events[0]
	if ev.Alertname != "InstanceDown" || !ev.Silenced || ev.Status != model.StatusFiring || ev.Source != "alertmanager" {
		t.Fatalf("%+v", ev)
	}
	if !ev.EndsAt.IsZero() {
		t.Fatalf("firing alert kept endsAt %s", ev.EndsAt)
	}
}

func TestFetchRejectsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, BearerToken: "tok"}
	_, err := c.Fetch(context.Background(), model.Window{End: time.Now()})
	if err == nil {
		t.Fatal("expected an error")
	}
}
