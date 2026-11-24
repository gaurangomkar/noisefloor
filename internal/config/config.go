// Package config loads the YAML file that describes sources, the scoring
// window, weights, thresholds, and grouping.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "time/tzdata" // so business-hour timezones work in minimal images

	"github.com/gaurangomkar/noisefloor/internal/duration"
)

// Config is a fully resolved configuration. Zero values are defaults; load
// paths through Load or Parse rather than filling this in from YAML by hand.
type Config struct {
	Window   time.Duration
	Now      time.Time
	Timezone string
	Location *time.Location
	BaseDir  string

	BusinessHours   BusinessHours
	Sources         Sources
	Rules           Rules
	Grouping        Grouping
	Ignore          Ignore
	Flap            Flap
	Scoring         Scoring
	Weights         Weights
	Thresholds      Thresholds
	Recommendations Recommendations
	Output          Output
	Webhook         Webhook
}

// BusinessHours is the window in which a page is not an off-hours page.
type BusinessHours struct {
	Enabled  bool
	Timezone string
	Location *time.Location
	Days     []time.Weekday
	StartMin int
	EndMin   int
}

// Contains reports whether t falls on a configured day between start and end.
// The end minute is exclusive.
func (b BusinessHours) Contains(t time.Time) bool {
	if !b.Enabled {
		return false
	}
	loc := b.Location
	if loc == nil {
		loc = time.UTC
	}
	local := t.In(loc)
	ok := false
	for _, d := range b.Days {
		if local.Weekday() == d {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	mins := local.Hour()*60 + local.Minute()
	return mins >= b.StartMin && mins < b.EndMin
}

// String is the hours as they appear in the report.
func (b BusinessHours) String() string {
	if !b.Enabled {
		return "disabled"
	}
	names := make([]string, len(b.Days))
	for i, d := range b.Days {
		names[i] = d.String()[:3]
	}
	tz := b.Timezone
	if tz == "" {
		tz = "UTC"
	}
	return fmt.Sprintf("%s %02d:%02d-%02d:%02d %s", strings.Join(names, ", "), b.StartMin/60, b.StartMin%60, b.EndMin/60, b.EndMin%60, tz)
}

// Sources are the places alert history is read from. Nil pointers are off.
type Sources struct {
	Alertmanager *Alertmanager
	SQLite       *SQLite
	Files        []FileSource
	PagerDuty    *PagerDuty
}

// Any reports whether report generation has somewhere to read.
func (s Sources) Any() bool {
	return s.Alertmanager != nil || s.SQLite != nil || len(s.Files) > 0 || s.PagerDuty != nil
}

// Alertmanager is one Alertmanager base URL.
type Alertmanager struct {
	URL              string
	Timeout          time.Duration
	Username         string
	Password         string
	BearerToken      string
	IncludeSilenced  bool
	IncludeInhibited bool
}

// SQLite is the webhook history file.
type SQLite struct {
	Path string
}

// FileSource is one JSON or NDJSON export.
type FileSource struct {
	Path string
	Name string
}

// PagerDuty is the optional incident source.
type PagerDuty struct {
	Token      string
	BaseURL    string
	ServiceIDs []string
	Timeout    time.Duration
}

// Rules lists Prometheus rule files or directories.
type Rules struct {
	Paths []string
}

// Grouping builds one report row per alert name plus these label values.
type Grouping struct {
	Labels   []string
	Fallback string
}

// Ignore drops matching instances before they are scored.
type Ignore struct {
	Alertnames map[string]struct{}
	Patterns   []*regexp.Regexp
	Matchers   []map[string]string
}

// Skip reports whether this alert should be left out of the score.
func (ig Ignore) Skip(alertname string, labels map[string]string) bool {
	if _, ok := ig.Alertnames[alertname]; ok {
		return true
	}
	for _, re := range ig.Patterns {
		if re.MatchString(alertname) {
			return true
		}
	}
	for _, m := range ig.Matchers {
		if labelsMatch(m, labels) {
			return true
		}
	}
	return false
}

func labelsMatch(want, got map[string]string) bool {
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return len(want) > 0
}

// Flap is the gap that still counts as the same noisy episode.
type Flap struct {
	Window time.Duration
}

// Scoring holds the normalizers behind the noise score.
type Scoring struct {
	VolumeCap                int
	ActionableAfter          time.Duration
	AutoResolveGrace         time.Duration
	OffHoursSeverities       []string
	CountUnspecifiedSeverity bool
}

// Weights scale each component. Zero removes that component from the score.
type Weights struct {
	Volume      float64
	Flap        float64
	ShortTTR    float64
	AutoResolve float64
	OffHours    float64
	Duplicate   float64
}

// Thresholds control the non-zero exit used in CI.
type Thresholds struct {
	MaxNoiseScore   float64
	MaxNoisyRules   int
	FailOnThreshold bool
}

// Recommendations are the cutoffs for tune, mute, and SLO.
type Recommendations struct {
	MinFires           int
	TuneFlapRate       float64
	TuneDuplicateRatio float64
	TuneOffHoursRate   float64
	MuteAutoResolve    float64
	SLOMinFires        int
}

// Output is where reports are written.
type Output struct {
	Dir      string
	Formats  []string
	Markdown string
	HTML     string
	JSON     string
}

// Filename returns the file name for a canonical format.
func (o Output) Filename(format string) string {
	switch format {
	case "html":
		return o.HTML
	case "json":
		return o.JSON
	default:
		return o.Markdown
	}
}

// Webhook is the listener that records Alertmanager notifications.
type Webhook struct {
	Addr  string
	Token string
}

// EffectiveNow returns the pinned end of the window, or fallback when the
// config does not pin one.
func (c Config) EffectiveNow(fallback time.Time) time.Time {
	if c.Now.IsZero() {
		return fallback.UTC()
	}
	return c.Now.UTC()
}

// Load reads a YAML file. Relative paths are resolved against the file's directory.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := Parse(b, filepath.Dir(abs))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes YAML and applies defaults. baseDir is the directory relative
// paths are resolved against.
func Parse(data []byte, baseDir string) (Config, error) {
	raw, err := decode(data)
	if err != nil {
		return Config{}, err
	}
	cfg, err := raw.resolve(baseDir)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	var ps []error
	add := func(cond bool, format string, args ...any) {
		if cond {
			ps = append(ps, fmt.Errorf(format, args...))
		}
	}
	add(c.Window <= 0, "window must be positive")
	add(c.Window > 366*24*time.Hour, "window must be 366 days or less")
	add(c.Flap.Window <= 0, "flap.window must be positive")
	add(c.Flap.Window > c.Window, "flap.window must be shorter than or equal to window")
	add(!c.Sources.Any(), "configure at least one source under sources")
	add(c.Scoring.VolumeCap < 1, "scoring.volume_cap must be at least 1")
	add(c.Scoring.ActionableAfter <= 0, "scoring.actionable_after must be positive")
	add(c.Scoring.AutoResolveGrace <= 0, "scoring.auto_resolve_grace must be positive")
	add(c.Weights.Volume < 0 || c.Weights.Flap < 0 || c.Weights.ShortTTR < 0 || c.Weights.AutoResolve < 0 || c.Weights.OffHours < 0 || c.Weights.Duplicate < 0, "weights must be zero or positive")
	add(c.Weights.Volume+c.Weights.Flap+c.Weights.ShortTTR+c.Weights.AutoResolve+c.Weights.OffHours+c.Weights.Duplicate == 0, "at least one weight must be positive")
	add(c.Thresholds.MaxNoiseScore < 0 || c.Thresholds.MaxNoiseScore > 100, "thresholds.max_noise_score must be between 0 and 100")
	add(c.Thresholds.MaxNoisyRules < 0, "thresholds.max_noisy_rules must be zero or positive")
	add(c.Recommendations.MinFires < 1, "recommendations.min_fires must be at least 1")
	add(c.Recommendations.SLOMinFires < 1, "recommendations.slo_min_fires must be at least 1")
	for _, name := range []struct {
		n string
		v float64
	}{
		{"recommendations.tune_flap_rate", c.Recommendations.TuneFlapRate},
		{"recommendations.tune_duplicate_ratio", c.Recommendations.TuneDuplicateRatio},
		{"recommendations.tune_off_hours_rate", c.Recommendations.TuneOffHoursRate},
		{"recommendations.mute_auto_resolve", c.Recommendations.MuteAutoResolve},
	} {
		add(name.v < 0 || name.v > 1, "%s must be between 0 and 1", name.n)
	}
	add(len(c.Grouping.Labels) == 0, "grouping.labels must not be empty")
	add(c.Grouping.Fallback == "", "grouping.fallback must not be empty")
	add(len(c.Output.Formats) == 0, "output.formats must not be empty")
	add(c.Output.Dir == "", "output.dir must not be empty")
	for _, f := range c.Output.Formats {
		switch f {
		case "markdown", "html", "json":
		default:
			add(true, "unknown output format %q", f)
		}
	}
	for _, name := range []string{c.Output.Markdown, c.Output.HTML, c.Output.JSON} {
		add(name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`), "output filenames must be a single path segment, got %q", name)
	}
	if c.Sources.Alertmanager != nil {
		add(!validHTTPURL(c.Sources.Alertmanager.URL), "sources.alertmanager.url must be an http or https URL")
	}
	if c.Sources.PagerDuty != nil {
		add(c.Sources.PagerDuty.Token == "", "sources.pagerduty is set but no token was found; set token or PAGERDUTY_TOKEN")
		add(!validHTTPURL(c.Sources.PagerDuty.BaseURL), "sources.pagerduty.base_url must be an http or https URL")
	}
	for _, f := range c.Sources.Files {
		add(f.Path == "", "sources.files path is empty")
	}
	if c.Sources.SQLite != nil {
		add(c.Sources.SQLite.Path == "", "sources.sqlite.path is empty")
	}
	if c.BusinessHours.Enabled {
		add(len(c.BusinessHours.Days) == 0, "business_hours.days must list at least one day")
		add(c.BusinessHours.EndMin <= c.BusinessHours.StartMin, "business_hours.end must be after start; shifts that cross midnight are not supported")
	}
	add(c.Webhook.Addr == "", "webhook.addr must not be empty")
	if len(ps) == 0 {
		return nil
	}
	msg := make([]string, len(ps))
	for i, err := range ps {
		msg[i] = err.Error()
	}
	return fmt.Errorf("invalid config:\n- %s", strings.Join(msg, "\n- "))
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func defaultWindow() time.Duration { return 7 * 24 * time.Hour }

func defaults() resolvedDefaults {
	return resolvedDefaults{
		timezone:         "UTC",
		businessTimezone: "UTC",
		businessDays:     []string{"mon", "tue", "wed", "thu", "fri"},
		businessStart:    "09:00",
		businessEnd:      "17:00",
		groupLabels:      []string{"team"},
		groupFallback:    "unowned",
		flap:             15 * time.Minute,
		volumeCap:        30,
		actionableAfter:  10 * time.Minute,
		autoResolveGrace: 5 * time.Minute,
		severities:       []string{"page", "critical", "high"},
		countUnspecified: true,
		weightVolume:     1,
		weightFlap:       2,
		weightShortTTR:   1.5,
		weightAuto:       2,
		weightOffHours:   1,
		weightDuplicate:  1.2,
		maxNoise:         60,
		failOnThreshold:  true,
		minFires:         5,
		tuneFlap:         0.30,
		tuneDuplicate:    0.30,
		tuneOffHours:     0.50,
		muteAuto:         0.75,
		sloMin:           15,
		outputDir:        ".",
		formats:          []string{"markdown", "html", "json"},
		markdownName:     "noisefloor.md",
		htmlName:         "noisefloor.html",
		jsonName:         "noisefloor.json",
		webhookAddr:      ":8080",
		webhookTokenEnv:  "NOISEFLOOR_WEBHOOK_TOKEN",
		pagerTokenEnv:    "PAGERDUTY_TOKEN",
		pagerBase:        "https://api.pagerduty.com",
		timeout:          30 * time.Second,
		includeSilenced:  true,
		includeInhibited: true,
		businessEnabled:  true,
	}
}

// resolvedDefaults exists so the YAML decoder and the tests share one set of numbers.
type resolvedDefaults struct {
	timezone         string
	businessTimezone string
	businessDays     []string
	businessStart    string
	businessEnd      string
	groupLabels      []string
	groupFallback    string
	flap             time.Duration
	volumeCap        int
	actionableAfter  time.Duration
	autoResolveGrace time.Duration
	severities       []string
	countUnspecified bool
	weightVolume     float64
	weightFlap       float64
	weightShortTTR   float64
	weightAuto       float64
	weightOffHours   float64
	weightDuplicate  float64
	maxNoise         float64
	failOnThreshold  bool
	minFires         int
	tuneFlap         float64
	tuneDuplicate    float64
	tuneOffHours     float64
	muteAuto         float64
	sloMin           int
	outputDir        string
	formats          []string
	markdownName     string
	htmlName         string
	jsonName         string
	webhookAddr      string
	webhookTokenEnv  string
	pagerTokenEnv    string
	pagerBase        string
	timeout          time.Duration
	includeSilenced  bool
	includeInhibited bool
	businessEnabled  bool
}

// ParseDuration is exposed for callers that already hold a config string.
func ParseDuration(s string) (time.Duration, error) {
	return duration.Parse(s)
}
