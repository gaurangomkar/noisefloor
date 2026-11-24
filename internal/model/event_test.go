package model

import (
	"testing"
	"time"
)

func TestNormalizeFiringDropsResolveTimeout(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	e := Normalize(Event{
		Status:   "active",
		StartsAt: start,
		EndsAt:   start.Add(5 * time.Minute),
		Labels:   map[string]string{"alertname": "Up", "instance": "a"},
	}, start.Add(time.Hour))
	if e.Status != StatusFiring {
		t.Fatalf("status %q", e.Status)
	}
	if !e.EndsAt.IsZero() {
		t.Fatalf("firing endsAt = %s, want zero", e.EndsAt)
	}
	if e.Alertname != "Up" {
		t.Fatalf("alertname %q", e.Alertname)
	}
	if e.Fingerprint == "" {
		t.Fatal("missing fingerprint")
	}
}

func TestNormalizeInfersResolved(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	e := Normalize(Event{
		StartsAt: start,
		EndsAt:   end,
		Labels:   map[string]string{"alertname": "Up"},
	}, end.Add(time.Minute))
	if e.Status != StatusResolved {
		t.Fatalf("status %q", e.Status)
	}
	d, ok := e.Duration()
	if !ok || d != 10*time.Minute {
		t.Fatalf("duration %s ok %v", d, ok)
	}
}

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint(map[string]string{"b": "2", "a": "1"})
	b := Fingerprint(map[string]string{"a": "1", "b": "2"})
	if a != b {
		t.Fatalf("fingerprints differ: %s %s", a, b)
	}
	if a == Fingerprint(map[string]string{"a": "1", "b": "3"}) {
		t.Fatal("fingerprint ignored a label change")
	}
}

func TestOverlapsAndStartedIn(t *testing.T) {
	w := Window{
		Start: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
	open := Event{Status: StatusFiring, StartsAt: w.Start.Add(-time.Hour)}
	if !open.Overlaps(w) || open.StartedIn(w) {
		t.Fatalf("open carried-in overlap %v started %v", open.Overlaps(w), open.StartedIn(w))
	}
	inside := Event{Status: StatusFiring, StartsAt: w.Start.Add(time.Hour)}
	if !inside.StartedIn(w) {
		t.Fatal("expected start inside the window")
	}
	after := Event{Status: StatusFiring, StartsAt: w.End}
	if after.Overlaps(w) || after.StartedIn(w) {
		t.Fatal("start at window end should be excluded")
	}
}
