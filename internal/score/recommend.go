package score

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/duration"
	"github.com/gaurangomkar/noisefloor/internal/model"
)

var (
	symptomRE = regexp.MustCompile(`(?i)(error(?:[_\s-]?rate)?|latency|duration|availability|5xx|4xx|apdex|saturation|timeout|errors)`)
	burnRE    = regexp.MustCompile(`(?i)(burn|error[_\s-]?budget)`)
)

func recommend(rs model.RuleScore, cfg config.Config, rulesLoaded bool) (string, []string) {
	m := rs.Metrics
	rec := cfg.Recommendations
	enough := m.Fires >= rec.MinFires

	var reasons []string
	switch {
	case enough && m.AutoResolveMethod != "none" && m.AutoResolve >= rec.MuteAutoResolve && m.FlapRate < rec.TuneFlapRate:
		reasons = append(reasons, muteReason(m, cfg))
		if symptom(rs) && !burn(rs) {
			reasons = append(reasons, "The rule looks like a user-facing symptom. If it should still page, convert it to a multi-window burn-rate alert and keep this rule as a non-paging warning.")
		}
		reasons = append(reasons, secondaryReasons(rs, cfg)...)
		reasons = append(reasons, contextReasons(rs, rulesLoaded)...)
		return model.RecMute, dedupe(reasons)
	case enough && m.Fires >= sloFloor(rec) && symptom(rs) && !burn(rs) && m.AutoResolve < rec.MuteAutoResolve:
		reasons = append(reasons, sloReason(rs))
		reasons = append(reasons, secondaryReasons(rs, cfg)...)
		reasons = append(reasons, contextReasons(rs, rulesLoaded)...)
		return model.RecSLO, dedupe(reasons)
	case enough && len(secondaryReasons(rs, cfg)) > 0:
		reasons = append(reasons, secondaryReasons(rs, cfg)...)
		reasons = append(reasons, contextReasons(rs, rulesLoaded)...)
		return model.RecTune, dedupe(reasons)
	case enough && rs.Score >= cfg.Thresholds.MaxNoiseScore:
		reasons = append(reasons, fmt.Sprintf("Noise score %.1f is at or above %.1f. Start with the highest component in the breakdown; no single flap, overlap, off-hours, or auto-resolve cutoff was crossed.", rs.Score, cfg.Thresholds.MaxNoiseScore))
		reasons = append(reasons, contextReasons(rs, rulesLoaded)...)
		return model.RecTune, dedupe(reasons)
	default:
		reasons = append(reasons, keepReason(rs, cfg))
		if !enough {
			reasons = append(reasons, secondaryReasons(rs, cfg)...)
		}
		reasons = append(reasons, contextReasons(rs, rulesLoaded)...)
		return model.RecKeep, dedupe(reasons)
	}
}

func sloFloor(rec config.Recommendations) int {
	if rec.SLOMinFires > rec.MinFires {
		return rec.SLOMinFires
	}
	return rec.MinFires
}

func muteReason(m model.Metrics, cfg config.Config) string {
	how := "measured from acknowledgement timestamps"
	if m.AutoResolveMethod == "grace" {
		how = fmt.Sprintf("no acknowledgement data; a resolution within %s counts as auto-resolve", cfg.Scoring.AutoResolveGrace)
	}
	return fmt.Sprintf("%.1f%% of resolved instances cleared before anyone acknowledged them (%s, median time to resolve %s). Stop paging on this rule: route it to a ticket or a dashboard, or delete it if the condition is not actionable.", m.AutoResolve*100, how, fmtTTR(m))
}

func sloReason(rs model.RuleScore) string {
	expr := "the alert name"
	if rs.Rule != nil && strings.TrimSpace(rs.Rule.Expr) != "" {
		expr = "`" + truncate(strings.Join(strings.Fields(rs.Rule.Expr), " "), 120) + "`"
	}
	return fmt.Sprintf("%d instances in the window on a symptom signal (%s). Page on a multi-window burn rate instead, for example 5m and 1h at 14.4x plus 30m and 6h at 6x, and keep a single-threshold rule as a ticket only.", rs.Metrics.Fires, expr)
}

// secondaryReasons lists flap, overlap, and off-hours problems that crossed a cutoff.
func secondaryReasons(rs model.RuleScore, cfg config.Config) []string {
	m := rs.Metrics
	rec := cfg.Recommendations
	var out []string
	if m.FlapRate >= rec.TuneFlapRate && m.Flaps > 0 {
		out = append(out, flapReason(rs, cfg))
	}
	if m.DuplicateRate >= rec.TuneDuplicateRatio && m.Duplicates > 0 {
		out = append(out, fmt.Sprintf("%d of %d instances overlapped a different fingerprint of this alert (%.1f%%). Aggregate the expression, or drop high-cardinality labels, so one incident produces one alert.", m.Duplicates, m.Fires, m.DuplicateRate*100))
	}
	if cfg.BusinessHours.Enabled && m.Pages > 0 && m.OffHoursRate >= rec.TuneOffHoursRate {
		out = append(out, fmt.Sprintf("%d of %d pages started outside business hours (%.1f%%). Page only during the working day, or open a ticket overnight and deliver it at the start of the next business day.", m.OffHoursPages, m.Pages, m.OffHoursRate*100))
	}
	return out
}

func flapReason(rs model.RuleScore, cfg config.Config) string {
	m := rs.Metrics
	forClause := ""
	if rs.Rule != nil && rs.Rule.For != "" && rs.Rule.For != "0s" {
		forClause = fmt.Sprintf(" (currently %s)", rs.Rule.For)
	}
	return fmt.Sprintf("%d of %d instances re-fired within %s of resolving (%.1f%%). Median time to resolve is %s. Raise `for`%s so the condition must hold through those blips, or set `keep_firing_for` to at least %s.", m.Flaps, m.Fires, pretty(cfg.Flap.Window), m.FlapRate*100, fmtTTR(m), forClause, pretty(cfg.Flap.Window))
}

func keepReason(rs model.RuleScore, cfg config.Config) string {
	m := rs.Metrics
	if rs.Score >= cfg.Thresholds.MaxNoiseScore && m.Fires < cfg.Recommendations.MinFires {
		return fmt.Sprintf("Noise score %.1f is at or above %.1f, but %d instances is below the minimum of %d for an automatic tune, mute, or SLO recommendation.", rs.Score, cfg.Thresholds.MaxNoiseScore, m.Fires, cfg.Recommendations.MinFires)
	}
	return fmt.Sprintf("Noise score %.1f is under the %.1f review threshold.", rs.Score, cfg.Thresholds.MaxNoiseScore)
}

func contextReasons(rs model.RuleScore, rulesLoaded bool) []string {
	m := rs.Metrics
	var out []string
	if rulesLoaded && rs.Rule == nil {
		out = append(out, "No Prometheus rule file matched this alert name, so the recommendation cannot cite the expression or the for duration.")
	}
	if m.Fires > 0 && m.Silenced*2 >= m.Fires {
		out = append(out, fmt.Sprintf("%d of %d instances were silenced. A long-lived silence is a sign the rule should be retuned or removed.", m.Silenced, m.Fires))
	}
	if m.StillFiring > 0 {
		out = append(out, fmt.Sprintf("%d instances were still firing at the end of the window.", m.StillFiring))
	}
	if m.CarriedIn > 0 {
		out = append(out, fmt.Sprintf("%d instances started before the window and were still open during it. They are not counted as new fires.", m.CarriedIn))
	}
	if m.TTRSamples > 0 && m.P90TTRSeconds >= (6*time.Hour).Seconds() {
		out = append(out, fmt.Sprintf("p90 time to resolve is %s. Check that the alert resolves when the condition clears.", fmtTTR(m)))
	}
	return out
}

func symptom(rs model.RuleScore) bool {
	expr := ""
	if rs.Rule != nil {
		expr = rs.Rule.Expr
	}
	return symptomRE.MatchString(rs.Alertname) || symptomRE.MatchString(expr)
}

func burn(rs model.RuleScore) bool {
	expr := ""
	if rs.Rule != nil {
		expr = rs.Rule.Expr
	}
	return burnRE.MatchString(rs.Alertname) || burnRE.MatchString(expr)
}

func fmtTTR(m model.Metrics) string {
	if m.TTRSamples == 0 {
		return "n/a"
	}
	d := time.Duration(m.MedianTTRSeconds * float64(time.Second)).Round(time.Second)
	return duration.Format(d)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "..."
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
