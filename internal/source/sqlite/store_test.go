package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestUpsertResolveAndAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "noisefloor.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	ctx := context.Background()
	firing := model.Normalize(model.Event{
		Fingerprint: "fp",
		Alertname:   "InstanceDown",
		Status:      model.StatusFiring,
		StartsAt:    start,
		EndsAt:      start.Add(5 * time.Minute),
		Labels:      map[string]string{"alertname": "InstanceDown", "instance": "a"},
		Source:      "webhook",
	}, start.Add(time.Hour))
	if err := s.Upsert(ctx, []model.Event{firing}); err != nil {
		t.Fatal(err)
	}
	resolved := firing
	resolved.Status = model.StatusResolved
	resolved.EndsAt = start.Add(12 * time.Minute)
	if err := s.Upsert(ctx, []model.Event{resolved, resolved}); err != nil {
		t.Fatal(err)
	}
	if err := s.Ack(ctx, "fp", start, start.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	res, err := reopened.Fetch(ctx, model.Window{Start: start.Add(-time.Hour), End: start.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("events %+v", res.Events)
	}
	ev := res.Events[0]
	if ev.Status != model.StatusResolved || !ev.EndsAt.Equal(start.Add(12*time.Minute)) {
		t.Fatalf("resolved %+v", ev)
	}
	if !ev.AckKnown || !ev.AckedAt.Equal(start.Add(3*time.Minute)) {
		t.Fatalf("ack %+v", ev)
	}
	if err := reopened.Ack(ctx, "missing", time.Time{}, time.Now()); err != ErrNotFound {
		t.Fatalf("err %v", err)
	}
}
