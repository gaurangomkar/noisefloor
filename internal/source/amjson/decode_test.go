package amjson

import (
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestParseWebhookAndAPI(t *testing.T) {
	now := time.Date(2026, 2, 23, 16, 0, 0, 0, time.UTC)
	body := []byte(`{
	  "receiver": "pager",
	  "status": "resolved",
	  "alerts": [{
	    "status": "resolved",
	    "labels": {"alertname": "InstanceDown", "instance": "web-1"},
	    "annotations": {"summary": "down"},
	    "startsAt": "2026-02-23T15:00:00Z",
	    "endsAt": "2026-02-23T15:10:00Z",
	    "generatorURL": "http://prom/graph",
	    "fingerprint": "abc"
	  }]
	}`)
	got, err := Parse(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len %d", len(got))
	}
	ev := got[0]
	if ev.Alertname != "InstanceDown" || ev.Receiver != "pager" || ev.Fingerprint != "abc" {
		t.Fatalf("%+v", ev)
	}
	if ev.Status != model.StatusResolved || ev.Source != "webhook" {
		t.Fatalf("status %s source %s", ev.Status, ev.Source)
	}

	api := []byte(`[{
	  "labels": {"alertname": "Up"},
	  "startsAt": "2026-02-23T15:00:00Z",
	  "endsAt": "2026-02-23T15:05:00Z",
	  "status": {"state": "active", "silencedBy": ["s1"], "inhibitedBy": []},
	  "receivers": [{"name": "slack"}]
	}]`)
	got, err = Parse(api, now)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Status != model.StatusFiring || !got[0].EndsAt.IsZero() || !got[0].Silenced || got[0].Receiver != "slack" {
		t.Fatalf("%+v", got[0])
	}
}

func TestParseNDJSONAck(t *testing.T) {
	now := time.Date(2026, 2, 23, 16, 0, 0, 0, time.UTC)
	body := []byte(`{"alertname":"A","status":"resolved","starts_at":"2026-02-23T15:00:00Z","ends_at":"2026-02-23T15:02:00Z","acked_at":null,"labels":{"alertname":"A"}}
{"alertname":"B","status":"resolved","starts_at":"2026-02-23T15:00:00Z","ends_at":"2026-02-23T15:20:00Z","acked_at":"2026-02-23T15:04:00Z","labels":{"alertname":"B"}}
`)
	got, err := Parse(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if !got[0].AckKnown || !got[0].AckedAt.IsZero() {
		t.Fatalf("first ack %+v", got[0])
	}
	if !got[1].AckKnown || got[1].AckedAt.IsZero() {
		t.Fatalf("second ack %+v", got[1])
	}
}

func TestParseRejectsMissingStart(t *testing.T) {
	_, err := Parse([]byte(`{"alertname":"A","labels":{"alertname":"A"}}`), time.Now())
	if err == nil {
		t.Fatal("expected an error")
	}
}
