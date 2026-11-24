// Package jsonfile reads alert history from a JSON or NDJSON file.
package jsonfile

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/source"
	"github.com/gaurangomkar/noisefloor/internal/source/amjson"
)

// Source reads one export file.
type Source struct {
	Path string
	// Name is the label used in the report. It defaults to file:<base>.
	Label string
	Now   func() time.Time
}

// Name implements source.Source.
func (s Source) Name() string {
	if s.Label != "" {
		return s.Label
	}
	return "file"
}

// Fetch implements source.Source. Events that do not overlap the window are omitted.
func (s Source) Fetch(ctx context.Context, window model.Window) (source.Result, error) {
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return source.Result{}, fmt.Errorf("open %s: %w", s.Path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, amjson.MaxBytes+1))
	if err != nil {
		return source.Result{}, fmt.Errorf("read %s: %w", s.Path, err)
	}
	if len(data) > amjson.MaxBytes {
		return source.Result{}, fmt.Errorf("read %s: file exceeds %d bytes", s.Path, amjson.MaxBytes)
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	events, err := amjson.Parse(data, now)
	if err != nil {
		return source.Result{}, fmt.Errorf("%s: %w", s.Path, err)
	}
	var (
		kept    []model.Event
		outside int
	)
	for _, ev := range events {
		if ev.Source == "" {
			ev.Source = "file"
		}
		if ev.Overlaps(window) {
			kept = append(kept, ev)
			continue
		}
		outside++
	}
	res := source.Result{Events: kept}
	if len(events) > 0 && len(kept) == 0 {
		res.Notes = []string{fmt.Sprintf("%s: %d events were outside the window", s.Name(), outside)}
	}
	return res, nil
}
