package jsonfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestFetchFiltersWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.ndjson")
	body := []byte(`{"alertname":"In","status":"firing","starts_at":"2026-02-23T15:00:00Z","labels":{"alertname":"In"}}
{"alertname":"Out","status":"resolved","starts_at":"2026-01-01T00:00:00Z","ends_at":"2026-01-01T01:00:00Z","labels":{"alertname":"Out"}}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	src := Source{Path: path, Label: "file:alerts.ndjson", Now: func() time.Time {
		return time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	}}
	res, err := src.Fetch(context.Background(), model.Window{
		Start: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].Alertname != "In" {
		t.Fatalf("%+v", res.Events)
	}
	if res.Events[0].Source != "file" {
		t.Fatalf("source %s", res.Events[0].Source)
	}
}
