package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gaurangomkar/noisefloor/internal/duration"
)

type rawConfig struct {
	Window          *string             `yaml:"window"`
	Now             *string             `yaml:"now"`
	Timezone        *string             `yaml:"timezone"`
	BusinessHours   *rawBusiness        `yaml:"business_hours"`
	Sources         rawSources          `yaml:"sources"`
	Rules           rawRules            `yaml:"rules"`
	Grouping        *rawGrouping        `yaml:"grouping"`
	Ignore          rawIgnore           `yaml:"ignore"`
	Flap            *rawFlap            `yaml:"flap"`
	Scoring         *rawScoring         `yaml:"scoring"`
	Weights         *rawWeights         `yaml:"weights"`
	Thresholds      *rawThresholds      `yaml:"thresholds"`
	Recommendations *rawRecommendations `yaml:"recommendations"`
	Output          *rawOutput          `yaml:"output"`
	Webhook         *rawWebhook         `yaml:"webhook"`
}

type rawBusiness struct {
	Enabled  *bool    `yaml:"enabled"`
	Timezone *string  `yaml:"timezone"`
	Days     []string `yaml:"days"`
	Start    *string  `yaml:"start"`
	End      *string  `yaml:"end"`
}

type rawSources struct {
	Alertmanager *rawAlertmanager `yaml:"alertmanager"`
	SQLite       *rawSQLite       `yaml:"sqlite"`
	Files        []rawFile        `yaml:"files"`
	PagerDuty    *rawPagerDuty    `yaml:"pagerduty"`
}

type rawAlertmanager struct {
	URL              string  `yaml:"url"`
	Timeout          *string `yaml:"timeout"`
	Username         string  `yaml:"username"`
	Password         string  `yaml:"password"`
	PasswordEnv      *string `yaml:"password_env"`
	BearerToken      string  `yaml:"bearer_token"`
	BearerTokenEnv   *string `yaml:"bearer_token_env"`
	BearerTokenFile  string  `yaml:"bearer_token_file"`
	IncludeSilenced  *bool   `yaml:"include_silenced"`
	IncludeInhibited *bool   `yaml:"include_inhibited"`
}

type rawSQLite struct {
	Path string `yaml:"path"`
}

type rawFile struct {
	Path string `yaml:"path"`
}

type rawPagerDuty struct {
	Token      string   `yaml:"token"`
	TokenEnv   *string  `yaml:"token_env"`
	BaseURL    *string  `yaml:"base_url"`
	ServiceIDs []string `yaml:"service_ids"`
	Timeout    *string  `yaml:"timeout"`
}

type rawRules struct {
	Paths []string `yaml:"paths"`
}

type rawGrouping struct {
	Labels   []string `yaml:"labels"`
	Fallback *string  `yaml:"fallback"`
}

type rawIgnore struct {
	Alertnames   []string     `yaml:"alertnames"`
	NamePatterns []string     `yaml:"name_patterns"`
	Matchers     []rawMatcher `yaml:"matchers"`
}

type rawMatcher struct {
	Labels map[string]string `yaml:"labels"`
}

type rawFlap struct {
	Window *string `yaml:"window"`
}

type rawScoring struct {
	VolumeCap                *int     `yaml:"volume_cap"`
	ActionableAfter          *string  `yaml:"actionable_after"`
	AutoResolveGrace         *string  `yaml:"auto_resolve_grace"`
	OffHoursSeverities       []string `yaml:"off_hours_severities"`
	CountUnspecifiedSeverity *bool    `yaml:"count_unspecified_severity"`
}

type rawWeights struct {
	Volume      *float64 `yaml:"volume"`
	Flap        *float64 `yaml:"flap"`
	ShortTTR    *float64 `yaml:"short_ttr"`
	AutoResolve *float64 `yaml:"auto_resolve"`
	OffHours    *float64 `yaml:"off_hours"`
	Duplicate   *float64 `yaml:"duplicate"`
}

type rawThresholds struct {
	MaxNoiseScore   *float64 `yaml:"max_noise_score"`
	MaxNoisyRules   *int     `yaml:"max_noisy_rules"`
	FailOnThreshold *bool    `yaml:"fail_on_threshold"`
}

type rawRecommendations struct {
	MinFires           *int     `yaml:"min_fires"`
	TuneFlapRate       *float64 `yaml:"tune_flap_rate"`
	TuneDuplicateRatio *float64 `yaml:"tune_duplicate_ratio"`
	TuneOffHoursRate   *float64 `yaml:"tune_off_hours_rate"`
	MuteAutoResolve    *float64 `yaml:"mute_auto_resolve"`
	SLOMinFires        *int     `yaml:"slo_min_fires"`
}

type rawOutput struct {
	Dir      *string  `yaml:"dir"`
	Formats  []string `yaml:"formats"`
	Markdown *string  `yaml:"markdown"`
	HTML     *string  `yaml:"html"`
	JSON     *string  `yaml:"json"`
}

type rawWebhook struct {
	Addr     *string `yaml:"addr"`
	Token    string  `yaml:"token"`
	TokenEnv *string `yaml:"token_env"`
}

func decode(data []byte) (rawConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawConfig
	if err := dec.Decode(&raw); err != nil {
		if err.Error() == "EOF" {
			return rawConfig{}, nil
		}
		return rawConfig{}, fmt.Errorf("parse yaml: %w", err)
	}
	return raw, nil
}

func (raw rawConfig) resolve(baseDir string) (Config, error) {
	d := defaults()
	var ps []error
	fail := func(err error) {
		if err != nil {
			ps = append(ps, err)
		}
	}

	window, err := optDuration(raw.Window, defaultWindow())
	fail(err)

	var now time.Time
	if raw.Now != nil && strings.TrimSpace(*raw.Now) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*raw.Now))
		if err != nil {
			fail(fmt.Errorf("now: %w", err))
		}
		now = now.UTC()
	}

	tzName := strPtr(raw.Timezone, d.timezone)
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		fail(fmt.Errorf("timezone: %w", err))
		loc = time.UTC
	}

	bh, err := resolveBusiness(raw.BusinessHours, d)
	fail(err)

	sources, err := resolveSources(raw.Sources, baseDir, d)
	fail(err)

	rules := Rules{Paths: append([]string(nil), raw.Rules.Paths...)}

	grouping := Grouping{Labels: append([]string(nil), d.groupLabels...), Fallback: d.groupFallback}
	if raw.Grouping != nil {
		if raw.Grouping.Labels != nil {
			grouping.Labels = append([]string(nil), raw.Grouping.Labels...)
		}
		if raw.Grouping.Fallback != nil {
			grouping.Fallback = *raw.Grouping.Fallback
		}
	}

	ignore, err := resolveIgnore(raw.Ignore)
	fail(err)

	flap := Flap{Window: d.flap}
	if raw.Flap != nil {
		flap.Window, err = optDuration(raw.Flap.Window, d.flap)
		fail(err)
	}

	scoring, err := resolveScoring(raw.Scoring, d)
	fail(err)
	weights := resolveWeights(raw.Weights, d)
	thresholds := resolveThresholds(raw.Thresholds, d)
	recs := resolveRecs(raw.Recommendations, d)
	output := resolveOutput(raw.Output, baseDir, d)
	webhook := resolveWebhook(raw.Webhook, d)

	if len(ps) > 0 {
		msg := make([]string, len(ps))
		for i, e := range ps {
			msg[i] = e.Error()
		}
		return Config{}, fmt.Errorf("invalid config:\n- %s", strings.Join(msg, "\n- "))
	}

	return Config{
		Window:          window,
		Now:             now,
		Timezone:        tzName,
		Location:        loc,
		BaseDir:         baseDir,
		BusinessHours:   bh,
		Sources:         sources,
		Rules:           rules,
		Grouping:        grouping,
		Ignore:          ignore,
		Flap:            flap,
		Scoring:         scoring,
		Weights:         weights,
		Thresholds:      thresholds,
		Recommendations: recs,
		Output:          output,
		Webhook:         webhook,
	}, nil
}

func resolveBusiness(raw *rawBusiness, d resolvedDefaults) (BusinessHours, error) {
	enabled := d.businessEnabled
	tzName := d.businessTimezone
	days := append([]string(nil), d.businessDays...)
	start := d.businessStart
	end := d.businessEnd
	if raw != nil {
		if raw.Enabled != nil {
			enabled = *raw.Enabled
		}
		if raw.Timezone != nil && strings.TrimSpace(*raw.Timezone) != "" {
			tzName = strings.TrimSpace(*raw.Timezone)
		}
		if raw.Days != nil {
			days = append([]string(nil), raw.Days...)
		}
		if raw.Start != nil {
			start = *raw.Start
		}
		if raw.End != nil {
			end = *raw.End
		}
	}
	bh := BusinessHours{Enabled: enabled, Timezone: tzName}
	if !enabled {
		return bh, nil
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return bh, fmt.Errorf("business_hours.timezone: %w", err)
	}
	bh.Location = loc
	seen := map[time.Weekday]bool{}
	for _, day := range days {
		wd, err := parseWeekday(day)
		if err != nil {
			return bh, err
		}
		if seen[wd] {
			continue
		}
		seen[wd] = true
		bh.Days = append(bh.Days, wd)
	}
	bh.StartMin, err = parseClock(start)
	if err != nil {
		return bh, fmt.Errorf("business_hours.start: %w", err)
	}
	bh.EndMin, err = parseClock(end)
	if err != nil {
		return bh, fmt.Errorf("business_hours.end: %w", err)
	}
	return bh, nil
}

func resolveSources(raw rawSources, base string, d resolvedDefaults) (Sources, error) {
	var out Sources
	var ps []error
	if raw.Alertmanager != nil {
		am, err := resolveAM(raw.Alertmanager, base, d)
		if err != nil {
			ps = append(ps, err)
		} else {
			out.Alertmanager = &am
		}
	}
	if raw.SQLite != nil {
		out.SQLite = &SQLite{Path: joinBase(base, raw.SQLite.Path)}
	}
	for i, f := range raw.Files {
		if strings.TrimSpace(f.Path) == "" {
			ps = append(ps, fmt.Errorf("sources.files[%d].path is empty", i))
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(f.Path))
		out.Files = append(out.Files, FileSource{
			Path: joinBase(base, f.Path),
			Name: "file:" + rel,
		})
	}
	if raw.PagerDuty != nil {
		pd, err := resolvePD(raw.PagerDuty, d)
		if err != nil {
			ps = append(ps, err)
		} else {
			out.PagerDuty = &pd
		}
	}
	if len(ps) > 0 {
		return out, ps[0]
	}
	return out, nil
}

func resolveAM(raw *rawAlertmanager, base string, d resolvedDefaults) (Alertmanager, error) {
	if raw.BearerToken != "" && raw.BearerTokenFile != "" {
		return Alertmanager{}, fmt.Errorf("sources.alertmanager: set bearer_token or bearer_token_file, not both")
	}
	token := raw.BearerToken
	if raw.BearerTokenFile != "" {
		b, err := os.ReadFile(joinBase(base, raw.BearerTokenFile))
		if err != nil {
			return Alertmanager{}, fmt.Errorf("sources.alertmanager.bearer_token_file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}
	token = firstNonEmpty(token, os.Getenv(strPtr(raw.BearerTokenEnv, "")))
	password := firstNonEmpty(raw.Password, os.Getenv(strPtr(raw.PasswordEnv, "")))
	timeout, err := optDuration(raw.Timeout, d.timeout)
	if err != nil {
		return Alertmanager{}, fmt.Errorf("sources.alertmanager.timeout: %w", err)
	}
	return Alertmanager{
		URL:              strings.TrimRight(strings.TrimSpace(raw.URL), "/"),
		Timeout:          timeout,
		Username:         raw.Username,
		Password:         password,
		BearerToken:      token,
		IncludeSilenced:  boolPtr(raw.IncludeSilenced, d.includeSilenced),
		IncludeInhibited: boolPtr(raw.IncludeInhibited, d.includeInhibited),
	}, nil
}

func resolvePD(raw *rawPagerDuty, d resolvedDefaults) (PagerDuty, error) {
	envName := strPtr(raw.TokenEnv, d.pagerTokenEnv)
	token := firstNonEmpty(strings.TrimSpace(raw.Token), os.Getenv(envName))
	timeout, err := optDuration(raw.Timeout, d.timeout)
	if err != nil {
		return PagerDuty{}, fmt.Errorf("sources.pagerduty.timeout: %w", err)
	}
	return PagerDuty{
		Token:      token,
		BaseURL:    strings.TrimRight(strPtr(raw.BaseURL, d.pagerBase), "/"),
		ServiceIDs: append([]string(nil), raw.ServiceIDs...),
		Timeout:    timeout,
	}, nil
}

func resolveIgnore(raw rawIgnore) (Ignore, error) {
	ig := Ignore{Alertnames: map[string]struct{}{}}
	for _, name := range raw.Alertnames {
		name = strings.TrimSpace(name)
		if name == "" {
			return ig, fmt.Errorf("ignore.alertnames contains an empty name")
		}
		ig.Alertnames[name] = struct{}{}
	}
	for _, pat := range raw.NamePatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return ig, fmt.Errorf("ignore.name_patterns %q: %w", pat, err)
		}
		ig.Patterns = append(ig.Patterns, re)
	}
	for i, m := range raw.Matchers {
		if len(m.Labels) == 0 {
			return ig, fmt.Errorf("ignore.matchers[%d] has no labels", i)
		}
		for k, v := range m.Labels {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
				return ig, fmt.Errorf("ignore.matchers[%d] has an empty label or value", i)
			}
		}
		cp := make(map[string]string, len(m.Labels))
		for k, v := range m.Labels {
			cp[k] = v
		}
		ig.Matchers = append(ig.Matchers, cp)
	}
	return ig, nil
}

func resolveScoring(raw *rawScoring, d resolvedDefaults) (Scoring, error) {
	s := Scoring{
		VolumeCap:                d.volumeCap,
		ActionableAfter:          d.actionableAfter,
		AutoResolveGrace:         d.autoResolveGrace,
		OffHoursSeverities:       append([]string(nil), d.severities...),
		CountUnspecifiedSeverity: d.countUnspecified,
	}
	if raw == nil {
		return s, nil
	}
	if raw.VolumeCap != nil {
		s.VolumeCap = *raw.VolumeCap
	}
	var err error
	s.ActionableAfter, err = optDuration(raw.ActionableAfter, d.actionableAfter)
	if err != nil {
		return s, fmt.Errorf("scoring.actionable_after: %w", err)
	}
	s.AutoResolveGrace, err = optDuration(raw.AutoResolveGrace, d.autoResolveGrace)
	if err != nil {
		return s, fmt.Errorf("scoring.auto_resolve_grace: %w", err)
	}
	if raw.OffHoursSeverities != nil {
		s.OffHoursSeverities = make([]string, len(raw.OffHoursSeverities))
		for i, sev := range raw.OffHoursSeverities {
			s.OffHoursSeverities[i] = strings.ToLower(strings.TrimSpace(sev))
		}
	}
	if raw.CountUnspecifiedSeverity != nil {
		s.CountUnspecifiedSeverity = *raw.CountUnspecifiedSeverity
	}
	return s, nil
}

func resolveWeights(raw *rawWeights, d resolvedDefaults) Weights {
	w := Weights{
		Volume: d.weightVolume, Flap: d.weightFlap, ShortTTR: d.weightShortTTR,
		AutoResolve: d.weightAuto, OffHours: d.weightOffHours, Duplicate: d.weightDuplicate,
	}
	if raw == nil {
		return w
	}
	w.Volume = floatPtr(raw.Volume, w.Volume)
	w.Flap = floatPtr(raw.Flap, w.Flap)
	w.ShortTTR = floatPtr(raw.ShortTTR, w.ShortTTR)
	w.AutoResolve = floatPtr(raw.AutoResolve, w.AutoResolve)
	w.OffHours = floatPtr(raw.OffHours, w.OffHours)
	w.Duplicate = floatPtr(raw.Duplicate, w.Duplicate)
	return w
}

func resolveThresholds(raw *rawThresholds, d resolvedDefaults) Thresholds {
	t := Thresholds{MaxNoiseScore: d.maxNoise, MaxNoisyRules: 0, FailOnThreshold: d.failOnThreshold}
	if raw == nil {
		return t
	}
	t.MaxNoiseScore = floatPtr(raw.MaxNoiseScore, t.MaxNoiseScore)
	if raw.MaxNoisyRules != nil {
		t.MaxNoisyRules = *raw.MaxNoisyRules
	}
	if raw.FailOnThreshold != nil {
		t.FailOnThreshold = *raw.FailOnThreshold
	}
	return t
}

func resolveRecs(raw *rawRecommendations, d resolvedDefaults) Recommendations {
	r := Recommendations{
		MinFires: d.minFires, TuneFlapRate: d.tuneFlap, TuneDuplicateRatio: d.tuneDuplicate,
		TuneOffHoursRate: d.tuneOffHours, MuteAutoResolve: d.muteAuto, SLOMinFires: d.sloMin,
	}
	if raw == nil {
		return r
	}
	if raw.MinFires != nil {
		r.MinFires = *raw.MinFires
	}
	r.TuneFlapRate = floatPtr(raw.TuneFlapRate, r.TuneFlapRate)
	r.TuneDuplicateRatio = floatPtr(raw.TuneDuplicateRatio, r.TuneDuplicateRatio)
	r.TuneOffHoursRate = floatPtr(raw.TuneOffHoursRate, r.TuneOffHoursRate)
	r.MuteAutoResolve = floatPtr(raw.MuteAutoResolve, r.MuteAutoResolve)
	if raw.SLOMinFires != nil {
		r.SLOMinFires = *raw.SLOMinFires
	}
	return r
}

func resolveOutput(raw *rawOutput, base string, d resolvedDefaults) Output {
	o := Output{
		Dir: joinBase(base, d.outputDir), Formats: append([]string(nil), d.formats...),
		Markdown: d.markdownName, HTML: d.htmlName, JSON: d.jsonName,
	}
	if raw == nil {
		return o
	}
	if raw.Dir != nil && strings.TrimSpace(*raw.Dir) != "" {
		o.Dir = joinBase(base, strings.TrimSpace(*raw.Dir))
	}
	if raw.Formats != nil {
		o.Formats = nil
		seen := map[string]bool{}
		for _, f := range raw.Formats {
			c := canonicalFormat(f)
			if seen[c] {
				continue
			}
			seen[c] = true
			o.Formats = append(o.Formats, c)
		}
	}
	if raw.Markdown != nil {
		o.Markdown = *raw.Markdown
	}
	if raw.HTML != nil {
		o.HTML = *raw.HTML
	}
	if raw.JSON != nil {
		o.JSON = *raw.JSON
	}
	return o
}

func resolveWebhook(raw *rawWebhook, d resolvedDefaults) Webhook {
	w := Webhook{Addr: d.webhookAddr}
	envName := d.webhookTokenEnv
	token := ""
	if raw != nil {
		if raw.Addr != nil && strings.TrimSpace(*raw.Addr) != "" {
			w.Addr = strings.TrimSpace(*raw.Addr)
		}
		token = raw.Token
		if raw.TokenEnv != nil {
			envName = *raw.TokenEnv
		}
	}
	w.Token = firstNonEmpty(strings.TrimSpace(token), os.Getenv(envName))
	return w
}

func canonicalFormat(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "md", "markdown":
		return "markdown"
	case "htm", "html":
		return "html"
	case "json":
		return "json"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func optDuration(p *string, def time.Duration) (time.Duration, error) {
	if p == nil || strings.TrimSpace(*p) == "" {
		return def, nil
	}
	d, err := duration.Parse(strings.TrimSpace(*p))
	if err != nil {
		return 0, err
	}
	return d, nil
}

func strPtr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func boolPtr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func floatPtr(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func joinBase(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	if base == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

func parseWeekday(s string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sun", "sunday", "0":
		return time.Sunday, nil
	case "mon", "monday", "1":
		return time.Monday, nil
	case "tue", "tues", "tuesday", "2":
		return time.Tuesday, nil
	case "wed", "wednesday", "3":
		return time.Wednesday, nil
	case "thu", "thur", "thurs", "thursday", "4":
		return time.Thursday, nil
	case "fri", "friday", "5":
		return time.Friday, nil
	case "sat", "saturday", "6":
		return time.Saturday, nil
	default:
		return 0, fmt.Errorf("business_hours.days: unknown day %q", s)
	}
}

func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("clock %q, want HH:MM", s)
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("clock %q, want HH:MM", s)
	}
	return h*60 + m, nil
}
