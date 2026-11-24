package source

import (
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestCollapsePrefersResolveAndEarliestAck(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	end := start.Add(20 * time.Minute)
	firing := model.Event{
		Fingerprint: "fp",
		Alertname:   "Up",
		Status:      model.StatusFiring,
		StartsAt:    start,
		Labels:      map[string]string{"alertname": "Up", "instance": "a"},
		Source:      "webhook",
	}
	resolved := model.Event{
		Fingerprint: "fp",
		Alertname:   "Up",
		Status:      model.StatusResolved,
		StartsAt:    start,
		EndsAt:      end,
		Labels:      map[string]string{"alertname": "Up", "job": "api"},
		Source:      "alertmanager",
		AckKnown:    true,
		AckedAt:     start.Add(5 * time.Minute),
		Silenced:    true,
	}
	laterAck := resolved
	laterAck.AckedAt = start.Add(8 * time.Minute)
	laterAck.Source = "pagerduty"

	got, dropped := Collapse([]model.Event{firing, resolved, laterAck, {Alertname: "NoStart"}})
	if dropped != 1 {
		t.Fatalf("dropped %d", dropped)
	}
	if len(got) != 1 {
		t.Fatalf("len %d", len(got))
	}
	ev := got[0]
	if ev.Status != model.StatusResolved || !ev.EndsAt.Equal(end) {
		t.Fatalf("resolve %+v", ev)
	}
	if !ev.AckedAt.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("ack %s", ev.AckedAt)
	}
	if ev.Labels["job"] != "api" || ev.Labels["instance"] != "a" {
		t.Fatalf("labels %v", ev.Labels)
	}
	if !ev.Silenced {
		t.Fatal("expected silence to stick")
	}
	if ev.Source != "alertmanager,pagerduty,webhook" {
		t.Fatalf("source %q", ev.Source)
	}
}
