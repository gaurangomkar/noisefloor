// Package run wires sources, rule files, and the scorer into one report.
package run

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/rules"
	"github.com/gaurangomkar/noisefloor/internal/score"
)

// Generate reads every configured source and scores the window that ends at
// cfg.Now, or at now when the config does not pin one.
func Generate(ctx context.Context, cfg config.Config, logger *slog.Logger, now time.Time) (model.Report, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	end := cfg.EffectiveNow(now)
	window := model.Window{Start: end.Add(-cfg.Window), End: end}

	sources, err := openSources(cfg)
	if err != nil {
		return model.Report{}, err
	}
	var (
		all   []model.Event
		stats []model.SourceStat
		notes []string
	)
	for _, src := range sources {
		res, err := src.Fetch(ctx, window)
		if err != nil {
			return model.Report{}, fmt.Errorf("%s: %w", src.Name(), err)
		}
		logger.Info("loaded source", "name", src.Name(), "events", len(res.Events))
		stats = append(stats, model.SourceStat{Name: src.Name(), Events: len(res.Events)})
		notes = append(notes, res.Notes...)
		all = append(all, res.Events...)
	}

	var idx *rules.Index
	if len(cfg.Rules.Paths) == 0 {
		notes = append(notes, "No Prometheus rule paths configured; recommendations cannot cite expressions.")
		idx = rules.Empty()
	} else {
		idx, err = rules.Load(cfg.BaseDir, cfg.Rules.Paths)
		if err != nil {
			return model.Report{}, err
		}
		notes = append(notes, fmt.Sprintf("Loaded %d alerting rule(s) from %d file(s).", idx.Count(), idx.Files()))
	}

	rep := score.Evaluate(cfg, all, idx, window)
	rep.Sources = stats
	rep.Notes = append(notes, rep.Notes...)
	rep.GeneratedAt = end
	return rep, nil
}
