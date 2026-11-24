// Package score turns alert instances into per-rule noise scores.
//
// The score is 100 times the weighted mean of six values, each in [0, 1]:
//
//	volume        fires / volume_cap, capped at 1
//	flap          flaps / fires
//	short_ttr     how far the median time-to-resolve sits below actionable_after
//	auto_resolve  fraction of resolved instances that cleared before acknowledgement
//	off_hours     off-hours pages / pages
//	duplicate     fires that overlapped a different fingerprint / fires
//
// A weight of zero drops that input. auto_resolve is dropped when no resolved
// sample exists. off_hours is dropped when business hours are disabled.
// The reported score is the sum of the per-component points after each point
// value is rounded to two decimals, so the table adds up to the score.
package score

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/rules"
	"github.com/gaurangomkar/noisefloor/internal/source"
)

// Evaluate scores the events that overlap window. Events are normalized and
// collapsed again so callers can pass raw notifications.
func Evaluate(cfg config.Config, events []model.Event, idx *rules.Index, window model.Window) model.Report {
	if idx == nil {
		idx = rules.Empty()
	}
	var (
		kept    []model.Event
		ignored int
		dropped int
	)
	for _, ev := range events {
		ev = model.Normalize(ev, window.End)
		if ev.StartsAt.IsZero() {
			dropped++
			continue
		}
		if !ev.Overlaps(window) {
			continue
		}
		if cfg.Ignore.Skip(ev.Alertname, ev.Labels) {
			ignored++
			continue
		}
		kept = append(kept, ev)
	}
	collapsed, extra := source.Collapse(kept)
	dropped += extra

	groups := map[string]*bucket{}
	var order []string
	for _, ev := range collapsed {
		key, team, labels := groupKey(ev, cfg)
		b := groups[key]
		if b == nil {
			b = &bucket{key: key, team: team, alertname: ev.Alertname, labels: labels}
			groups[key] = b
			order = append(order, key)
		}
		b.events = append(b.events, ev)
	}

	rulesOut := make([]model.RuleScore, 0, len(order))
	for _, key := range order {
		b := groups[key]
		in := inWindow(b.events, window)
		if len(in) == 0 {
			continue
		}
		rs := scoreBucket(cfg, b, in, window, idx)
		rulesOut = append(rulesOut, rs)
	}
	sort.Slice(rulesOut, func(i, j int) bool {
		if rulesOut[i].Score != rulesOut[j].Score {
			return rulesOut[i].Score > rulesOut[j].Score
		}
		if rulesOut[i].Alertname != rulesOut[j].Alertname {
			return rulesOut[i].Alertname < rulesOut[j].Alertname
		}
		return rulesOut[i].GroupKey < rulesOut[j].GroupKey
	})

	summary := summarize(cfg, rulesOut, len(collapsed), ignored, dropped)
	return model.Report{
		ScorerVersion: model.ScorerVersion,
		GeneratedAt:   window.End.UTC(),
		WindowStart:   window.Start.UTC(),
		WindowEnd:     window.End.UTC(),
		Timezone:      tzName(cfg),
		Summary:       summary,
		Rules:         rulesOut,
		Notes:         notes(cfg, summary),
	}
}

type bucket struct {
	key       string
	alertname string
	team      string
	labels    map[string]string
	events    []model.Event
}

func scoreBucket(cfg config.Config, b *bucket, in []model.Event, window model.Window, idx *rules.Index) model.RuleScore {
	m := measure(cfg, in, b.events, window)
	var match *model.RuleMatch
	if rule, ok := idx.Lookup(b.alertname, in[0].Labels); ok {
		match = rule.Match()
	}
	comps, total := components(cfg, m)
	rs := model.RuleScore{
		Alertname:  b.alertname,
		Group:      b.labels,
		GroupKey:   b.key,
		Team:       b.team,
		Score:      total,
		Metrics:    m,
		Components: comps,
		Rule:       match,
		Examples:   examples(in),
		Reasons:    []string{},
	}
	rs.Recommendation, rs.Reasons = recommend(rs, cfg, idx.Count() > 0)
	return rs
}

func measure(cfg config.Config, in, all []model.Event, window model.Window) model.Metrics {
	m := model.Metrics{Fires: len(in)}
	for _, ev := range all {
		if !ev.StartedIn(window) && ev.Overlaps(window) {
			m.CarriedIn++
		}
	}
	flaps := flapSet(all, cfg.Flap.Window)
	var ttrs []time.Duration
	var known bool
	for _, ev := range in {
		if ev.AckKnown {
			known = true
		}
		if flaps[instanceID(ev)] {
			m.Flaps++
		}
		if d, ok := ev.Duration(); ok {
			m.Resolved++
			ttrs = append(ttrs, d)
		} else {
			m.StillFiring++
		}
		if ev.Silenced {
			m.Silenced++
		}
		if ev.Inhibited {
			m.Inhibited++
		}
		if isPage(ev, cfg) {
			m.Pages++
			if cfg.BusinessHours.Enabled && !cfg.BusinessHours.Contains(ev.StartsAt) {
				m.OffHoursPages++
			}
		}
		if overlapsOther(ev, all, window.End) {
			m.Duplicates++
		}
	}
	if m.Fires > 0 {
		m.FlapRate = round4(float64(m.Flaps) / float64(m.Fires))
		m.DuplicateRate = round4(float64(m.Duplicates) / float64(m.Fires))
	}
	if m.Pages > 0 {
		m.OffHoursRate = round4(float64(m.OffHoursPages) / float64(m.Pages))
	}
	sort.Slice(ttrs, func(i, j int) bool { return ttrs[i] < ttrs[j] })
	m.TTRSamples = len(ttrs)
	if len(ttrs) > 0 {
		m.MedianTTRSeconds = round4(percentile(ttrs, 0.50).Seconds())
		m.P90TTRSeconds = round4(percentile(ttrs, 0.90).Seconds())
	}
	fillAuto(&m, in, known, cfg.Scoring.AutoResolveGrace)
	return m
}

func fillAuto(m *model.Metrics, in []model.Event, known bool, grace time.Duration) {
	var samples, auto int
	if known {
		m.AutoResolveMethod = "ack"
		for _, ev := range in {
			if !ev.AckKnown || !ev.Resolved() {
				continue
			}
			samples++
			if ev.AckedAt.IsZero() || !ev.AckedAt.Before(ev.EndsAt) {
				auto++
			}
		}
	} else {
		m.AutoResolveMethod = "grace"
		for _, ev := range in {
			d, ok := ev.Duration()
			if !ok {
				continue
			}
			samples++
			if d <= grace {
				auto++
			}
		}
	}
	m.AutoResolveSamples = samples
	if samples == 0 {
		m.AutoResolveMethod = "none"
		return
	}
	m.AutoResolve = round4(float64(auto) / float64(samples))
}

func flapSet(all []model.Event, window time.Duration) map[string]bool {
	by := map[string][]model.Event{}
	for _, ev := range all {
		by[ev.Fingerprint] = append(by[ev.Fingerprint], ev)
	}
	out := map[string]bool{}
	for _, list := range by {
		sort.Slice(list, func(i, j int) bool { return list[i].StartsAt.Before(list[j].StartsAt) })
		for i := 1; i < len(list); i++ {
			prev := list[i-1]
			if !prev.Resolved() {
				continue
			}
			gap := list[i].StartsAt.Sub(prev.EndsAt)
			if gap >= 0 && gap <= window {
				out[instanceID(list[i])] = true
			}
		}
	}
	return out
}

func overlapsOther(ev model.Event, all []model.Event, windowEnd time.Time) bool {
	a0, a1 := ev.StartsAt, ev.Ends(windowEnd)
	for _, other := range all {
		if other.Fingerprint == ev.Fingerprint {
			continue
		}
		b0, b1 := other.StartsAt, other.Ends(windowEnd)
		if a0.Before(b1) && b0.Before(a1) {
			return true
		}
	}
	return false
}

func components(cfg config.Config, m model.Metrics) ([]model.Component, float64) {
	type part struct {
		name   string
		weight float64
		value  float64
	}
	var parts []part
	add := func(name string, weight, value float64) {
		if weight <= 0 {
			return
		}
		parts = append(parts, part{name: name, weight: weight, value: clamp01(value)})
	}
	capN := cfg.Scoring.VolumeCap
	if capN < 1 {
		capN = 1
	}
	add("volume", cfg.Weights.Volume, float64(m.Fires)/float64(capN))
	add("flap", cfg.Weights.Flap, m.FlapRate)
	add("short_ttr", cfg.Weights.ShortTTR, shortTTR(m, cfg.Scoring.ActionableAfter))
	if m.AutoResolveMethod != "none" {
		add("auto_resolve", cfg.Weights.AutoResolve, m.AutoResolve)
	}
	if cfg.BusinessHours.Enabled {
		add("off_hours", cfg.Weights.OffHours, m.OffHoursRate)
	}
	add("duplicate", cfg.Weights.Duplicate, m.DuplicateRate)

	var sumW float64
	for _, p := range parts {
		sumW += p.weight
	}
	comps := make([]model.Component, 0, len(parts))
	var total float64
	for _, p := range parts {
		points := 0.0
		if sumW > 0 {
			points = round2(100 * p.weight * p.value / sumW)
		}
		total += points
		comps = append(comps, model.Component{
			Name:   p.name,
			Weight: p.weight,
			Value:  round4(p.value),
			Points: points,
		})
	}
	sort.Slice(comps, func(i, j int) bool {
		if comps[i].Points != comps[j].Points {
			return comps[i].Points > comps[j].Points
		}
		return comps[i].Name < comps[j].Name
	})
	return comps, round2(total)
}

func shortTTR(m model.Metrics, actionable time.Duration) float64 {
	if m.TTRSamples == 0 || actionable <= 0 {
		return 0
	}
	median := time.Duration(m.MedianTTRSeconds * float64(time.Second))
	if median >= actionable {
		return 0
	}
	return float64(actionable-median) / float64(actionable)
}

func summarize(cfg config.Config, rulesOut []model.RuleScore, events, ignored, dropped int) model.Summary {
	recs := map[string]int{
		model.RecTune: 0,
		model.RecMute: 0,
		model.RecSLO:  0,
		model.RecKeep: 0,
	}
	noisy := 0
	for _, rs := range rulesOut {
		recs[rs.Recommendation]++
		if rs.Score >= cfg.Thresholds.MaxNoiseScore {
			noisy++
		}
	}
	sum := model.Summary{
		Events:            events,
		Ignored:           ignored,
		Dropped:           dropped,
		Rules:             len(rulesOut),
		NoisyRules:        noisy,
		MaxNoiseScore:     cfg.Thresholds.MaxNoiseScore,
		MaxNoisyRules:     cfg.Thresholds.MaxNoisyRules,
		ThresholdExceeded: noisy > cfg.Thresholds.MaxNoisyRules,
		Recommendations:   recs,
		BusinessHours:     cfg.BusinessHours.String(),
		FlapWindow:        pretty(cfg.Flap.Window),
		AutoResolveGrace:  pretty(cfg.Scoring.AutoResolveGrace),
		VolumeCap:         cfg.Scoring.VolumeCap,
		ActionableAfter:   pretty(cfg.Scoring.ActionableAfter),
	}
	if len(rulesOut) > 0 {
		sum.WorstScore = rulesOut[0].Score
		sum.WorstRule = rulesOut[0].Alertname
		if rulesOut[0].Team != "" {
			sum.WorstRule = rulesOut[0].Alertname + " (" + rulesOut[0].Team + ")"
		}
	}
	return sum
}

func notes(cfg config.Config, sum model.Summary) []string {
	sev := "unspecified"
	if len(cfg.Scoring.OffHoursSeverities) > 0 {
		sev = strings.Join(cfg.Scoring.OffHoursSeverities, ", ")
	}
	pageNote := fmt.Sprintf("A page is an alert whose severity is one of: %s.", sev)
	if cfg.Scoring.CountUnspecifiedSeverity {
		pageNote = fmt.Sprintf("A page is an alert whose severity is one of: %s. Alerts with no severity label count as pages.", sev)
	}
	return []string{
		fmt.Sprintf("Scored %d instances after ignoring %d and dropping %d with no start time.", sum.Events, sum.Ignored, sum.Dropped),
		fmt.Sprintf("A flap is a firing that starts within %s after the previous instance of the same fingerprint resolved.", pretty(cfg.Flap.Window)),
		fmt.Sprintf("Business hours: %s.", cfg.BusinessHours),
		pageNote,
		fmt.Sprintf("Auto-resolve uses acknowledgement timestamps when a source recorded them. Otherwise a resolution within %s counts as auto-resolve.", pretty(cfg.Scoring.AutoResolveGrace)),
		fmt.Sprintf("The noise score is 100 times the weighted mean of volume (fires / %d, capped at 1), flap rate, short time-to-resolve, auto-resolve ratio, off-hours page ratio, and overlap ratio.", cfg.Scoring.VolumeCap),
	}
}

func groupKey(ev model.Event, cfg config.Config) (string, string, map[string]string) {
	labels := make(map[string]string, len(cfg.Grouping.Labels))
	var b strings.Builder
	b.WriteString(ev.Alertname)
	vals := make([]string, 0, len(cfg.Grouping.Labels))
	for _, key := range cfg.Grouping.Labels {
		val := ev.Labels[key]
		if val == "" {
			val = cfg.Grouping.Fallback
		}
		labels[key] = val
		vals = append(vals, val)
		b.WriteByte('\x1f')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(val)
	}
	team := strings.Join(vals, " / ")
	if team == "" {
		team = cfg.Grouping.Fallback
	}
	return b.String(), team, labels
}

func inWindow(events []model.Event, window model.Window) []model.Event {
	var out []model.Event
	for _, ev := range events {
		if ev.StartedIn(window) {
			out = append(out, ev)
		}
	}
	return out
}

func examples(in []model.Event) []map[string]string {
	seen := map[string]map[string]string{}
	var keys []string
	for _, ev := range in {
		k := labelKey(ev.Labels)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = copyLabels(ev.Labels)
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 3 {
		keys = keys[:3]
	}
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

func labelKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte(0)
	}
	return b.String()
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isPage(ev model.Event, cfg config.Config) bool {
	sev := strings.ToLower(ev.Labels["severity"])
	if sev == "" {
		return cfg.Scoring.CountUnspecifiedSeverity
	}
	for _, want := range cfg.Scoring.OffHoursSeverities {
		if sev == want {
			return true
		}
	}
	return false
}

func instanceID(ev model.Event) string {
	return ev.Fingerprint + "\x00" + ev.StartsAt.UTC().Format(time.RFC3339Nano)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return time.Duration(float64(sorted[lo])*(1-frac) + float64(sorted[hi])*frac)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

func pretty(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.Round(time.Second).String()
}

func tzName(cfg config.Config) string {
	if cfg.Timezone == "" {
		return "UTC"
	}
	return cfg.Timezone
}
