package model

import "time"

// ScorerVersion is stored on every report so a saved JSON file can be
// compared with the scorer that produced it.
const ScorerVersion = "1"

// Report is the scored week. Renderers do not recompute it.
type Report struct {
	ScorerVersion string       `json:"scorer_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	WindowStart   time.Time    `json:"window_start"`
	WindowEnd     time.Time    `json:"window_end"`
	Timezone      string       `json:"timezone"`
	Sources       []SourceStat `json:"sources"`
	Summary       Summary      `json:"summary"`
	Rules         []RuleScore  `json:"rules"`
	Notes         []string     `json:"notes"`
}

// SourceStat is how many events a source returned before dedupe.
type SourceStat struct {
	Name   string `json:"name"`
	Events int    `json:"events"`
}

// Summary is the week in one block, including the CI threshold result.
type Summary struct {
	Events            int            `json:"events"`
	Ignored           int            `json:"ignored"`
	Dropped           int            `json:"dropped"`
	Rules             int            `json:"rules"`
	NoisyRules        int            `json:"noisy_rules"`
	MaxNoiseScore     float64        `json:"max_noise_score"`
	MaxNoisyRules     int            `json:"max_noisy_rules"`
	ThresholdExceeded bool           `json:"threshold_exceeded"`
	WorstScore        float64        `json:"worst_score"`
	WorstRule         string         `json:"worst_rule"`
	Recommendations   map[string]int `json:"recommendations"`
	BusinessHours     string         `json:"business_hours"`
	FlapWindow        string         `json:"flap_window"`
	AutoResolveGrace  string         `json:"auto_resolve_grace"`
	VolumeCap         int            `json:"volume_cap"`
	ActionableAfter   string         `json:"actionable_after"`
}

// RuleScore is one alert name plus the configured grouping labels.
type RuleScore struct {
	Alertname      string              `json:"alertname"`
	Group          map[string]string   `json:"group"`
	GroupKey       string              `json:"group_key"`
	Team           string              `json:"team"`
	Score          float64             `json:"score"`
	Recommendation string              `json:"recommendation"`
	Reasons        []string            `json:"reasons"`
	Metrics        Metrics             `json:"metrics"`
	Components     []Component         `json:"components"`
	Rule           *RuleMatch          `json:"rule,omitempty"`
	Examples       []map[string]string `json:"examples"`
}

// RuleMatch is the Prometheus rule that produced the alert, when one was found.
type RuleMatch struct {
	File          string            `json:"file"`
	Group         string            `json:"group"`
	Line          int               `json:"line,omitempty"`
	Expr          string            `json:"expr,omitempty"`
	For           string            `json:"for,omitempty"`
	KeepFiringFor string            `json:"keep_firing_for,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Metrics are the raw measurements. Rates are fractions in [0, 1].
// Duration seconds are zero when TTRSamples is zero.
type Metrics struct {
	Fires              int     `json:"fires"`
	Flaps              int     `json:"flaps"`
	FlapRate           float64 `json:"flap_rate"`
	Resolved           int     `json:"resolved"`
	StillFiring        int     `json:"still_firing"`
	CarriedIn          int     `json:"carried_in"`
	MedianTTRSeconds   float64 `json:"median_ttr_seconds"`
	P90TTRSeconds      float64 `json:"p90_ttr_seconds"`
	TTRSamples         int     `json:"ttr_samples"`
	AutoResolve        float64 `json:"auto_resolve_ratio"`
	AutoResolveMethod  string  `json:"auto_resolve_method"`
	AutoResolveSamples int     `json:"auto_resolve_samples"`
	Pages              int     `json:"pages"`
	OffHoursPages      int     `json:"off_hours_pages"`
	OffHoursRate       float64 `json:"off_hours_rate"`
	Duplicates         int     `json:"duplicates"`
	DuplicateRate      float64 `json:"duplicate_rate"`
	Silenced           int     `json:"silenced"`
	Inhibited          int     `json:"inhibited"`
}

// Component is one weighted input to the noise score.
// Points are that input's contribution to the 0-100 score.
type Component struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Value  float64 `json:"value"`
	Points float64 `json:"points"`
}

// ExceedsThreshold reports whether CI should fail this report.
// MaxNoisyRules is the number of rules allowed to sit at or above MaxNoiseScore.
// The default of zero means any noisy rule fails the run.
func (r Report) ExceedsThreshold() bool {
	return r.Summary.ThresholdExceeded
}
