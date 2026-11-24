// Package model holds the alert instances, rule matches, and report types
// shared by sources, the scorer, and the renderers.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

const (
	StatusFiring   = "firing"
	StatusResolved = "resolved"

	RecTune = "tune"
	RecMute = "mute"
	RecSLO  = "slo"
	RecKeep = "keep"
)

// Window is the half-open interval [Start, End) the report covers.
type Window struct {
	Start time.Time
	End   time.Time
}

// Event is one firing of an alert: a fingerprint plus the time it started.
// Notifications for the same firing collapse into one event before scoring.
type Event struct {
	Fingerprint  string
	Alertname    string
	Status       string
	StartsAt     time.Time
	EndsAt       time.Time
	Labels       map[string]string
	Annotations  map[string]string
	GeneratorURL string
	Receiver     string
	Source       string
	AckedAt      time.Time
	// AckKnown is true when the source recorded acknowledgement state, including
	// an explicit "never acknowledged". It is false when the source cannot tell.
	AckKnown  bool
	Silenced  bool
	Inhibited bool
}

// Normalize fills the alert name, canonical status, and fingerprint, and drops
// the resolve-timeout endsAt that Alertmanager attaches to alerts that are
// still firing. now is the moment used when a missing status has to be inferred.
func Normalize(e Event, now time.Time) Event {
	if e.Labels == nil {
		e.Labels = map[string]string{}
	} else {
		e.Labels = cloneMap(e.Labels)
	}
	e.Annotations = cloneMap(e.Annotations)
	if e.Alertname == "" {
		e.Alertname = e.Labels["alertname"]
	}
	if e.Alertname != "" {
		e.Labels["alertname"] = e.Alertname
	}
	e.StartsAt = e.StartsAt.UTC()
	e.EndsAt = e.EndsAt.UTC()
	e.AckedAt = e.AckedAt.UTC()
	if e.AckedAt.IsZero() {
		e.AckedAt = time.Time{}
	}
	e.Status = canonicalizeStatus(e.Status)
	if e.Status == "" {
		if !e.EndsAt.IsZero() && !e.EndsAt.After(now) {
			e.Status = StatusResolved
		} else {
			e.Status = StatusFiring
		}
	}
	// A firing alert's endsAt is Alertmanager's resolve timeout, not a resolution.
	if e.Status != StatusResolved {
		e.EndsAt = time.Time{}
	}
	if e.Fingerprint == "" {
		e.Fingerprint = Fingerprint(e.Labels)
	}
	return e
}

func canonicalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ""
	case StatusResolved, "ok":
		return StatusResolved
	case StatusFiring, "active", "suppressed", "unprocessed":
		return StatusFiring
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// Fingerprint is a stable 16-hex-character hash of the label set.
// Alertmanager's own fingerprint is preferred when the source provides one.
func Fingerprint(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(labels[k]))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// Resolved reports whether this instance has a usable resolution time.
func (e Event) Resolved() bool {
	return e.Status == StatusResolved && !e.EndsAt.IsZero()
}

// Ends returns when the instance stopped occupying the timeline.
// Open instances are treated as open through windowEnd.
func (e Event) Ends(windowEnd time.Time) time.Time {
	if e.Resolved() {
		return e.EndsAt
	}
	return windowEnd
}

// Overlaps reports whether the instance was active during the half-open window.
func (e Event) Overlaps(w Window) bool {
	if e.StartsAt.IsZero() || !e.StartsAt.Before(w.End) {
		return false
	}
	return e.Ends(w.End).After(w.Start)
}

// StartedIn reports whether the instance started during the half-open window.
func (e Event) StartedIn(w Window) bool {
	return !e.StartsAt.Before(w.Start) && e.StartsAt.Before(w.End)
}

// Duration is the time from fire to resolve.
func (e Event) Duration() (time.Duration, bool) {
	if !e.Resolved() || e.EndsAt.Before(e.StartsAt) {
		return 0, false
	}
	return e.EndsAt.Sub(e.StartsAt), true
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
