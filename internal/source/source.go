// Package source defines how alert history is loaded.
//
// A Source returns alert instances that overlap a window. Implementations must
// be safe to call once per report; they do not have to be safe for concurrent
// Fetch calls on the same value.
package source

import (
	"context"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

// Result is one read from a source.
type Result struct {
	Events []model.Event
	Notes  []string
}

// Source loads alert instances.
type Source interface {
	Name() string
	Fetch(ctx context.Context, window model.Window) (Result, error)
}
