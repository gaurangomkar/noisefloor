package pagerduty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestFetchPaginatesAndMapsAcks(t *testing.T) {
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token token=sekret" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("service_ids[]") != "PSVC" {
			t.Errorf("services %s", r.URL.RawQuery)
		}
		pages++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "0" {
			_, _ = w.Write([]byte(`{
			  "more": true,
			  "incidents": [{
			    "id": "P1",
			    "title": "[FIRING:2] CheckoutLatencyHigh payments",
			    "status": "resolved",
			    "urgency": "high",
			    "created_at": "2026-02-23T15:00:00Z",
			    "resolved_at": "2026-02-23T15:30:00Z",
			    "html_url": "https://example.pagerduty.com/incidents/P1",
			    "acknowledgements": [{"at": "2026-02-23T15:20:00Z"}, {"at": "2026-02-23T15:05:00Z"}],
			    "service": {"id": "PSVC", "summary": "checkout"}
			  }]
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
		  "more": false,
		  "incidents": [{
		    "id": "P2",
		    "title": "disk is filling up",
		    "status": "triggered",
		    "urgency": "low",
		    "created_at": "2026-02-24T15:00:00Z",
		    "service": {"id": "PSVC", "summary": "checkout"}
		  }]
		}`))
	}))
	defer srv.Close()

	c := Client{Token: "sekret", BaseURL: srv.URL, ServiceIDs: []string{"PSVC"}}
	res, err := c.Fetch(context.Background(), model.Window{
		Start: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(res.Events) != 2 {
		t.Fatalf("pages %d events %d", pages, len(res.Events))
	}
	first := res.Events[0]
	if first.Alertname != "CheckoutLatencyHigh" || first.Status != model.StatusResolved {
		t.Fatalf("first %+v", first)
	}
	if !first.AckKnown || !first.AckedAt.Equal(time.Date(2026, 2, 23, 15, 5, 0, 0, time.UTC)) {
		t.Fatalf("ack %s known %v", first.AckedAt, first.AckKnown)
	}
	if first.Labels["severity"] != "page" || first.Labels["service"] != "checkout" {
		t.Fatalf("labels %v", first.Labels)
	}
	if res.Events[1].Alertname != "disk" || res.Events[1].Labels["severity"] != "info" || res.Events[1].Status != model.StatusFiring {
		t.Fatalf("second %+v", res.Events[1])
	}
}

func TestAlertName(t *testing.T) {
	if got := alertName("[RESOLVED] APIDown"); got != "APIDown" {
		t.Fatalf("%s", got)
	}
	if got := alertName("something went wrong"); got != "something" {
		t.Fatalf("%s", got)
	}
	if got := alertName("503 from the edge"); got != "503 from the edge" {
		t.Fatalf("%s", got)
	}
}

func TestFetchRequiresToken(t *testing.T) {
	_, err := (Client{}).Fetch(context.Background(), model.Window{})
	if err == nil {
		t.Fatal("expected an error")
	}
}
