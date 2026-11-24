package score

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/rules"
)

func TestFlapPercentileAndTune(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	var events []model.Event
	// Five resolved fires, 1m apart after a 1m duration, then one more an hour later.
	// Gaps of 1m are flaps; the hour gap is not. Durations 1m..5m for the percentile check
	// are a separate series below.
	for i := 0; i < 5; i++ {
		s := start.Add(time.Duration(i) * 2 * time.Minute)
		events = append(events, firing("fp", "InstanceDown", s, s.Add(time.Minute), map[string]string{
			"alertname": "InstanceDown", "severity": "page", "team": "platform",
		}))
	}
	idx := loadRules(t, `
groups:
- name: platform
  rules:
  - alert: InstanceDown
    expr: up == 0
    for: 30s
    labels:
      team: platform
`)
	rep := Evaluate(testCfg(), events, idx, week())
	if len(rep.Rules) != 1 {
		t.Fatalf("rules %d", len(rep.Rules))
	}
	rs := rep.Rules[0]
	if rs.Metrics.Fires != 5 || rs.Metrics.Flaps != 4 {
		t.Fatalf("fires %d flaps %d", rs.Metrics.Fires, rs.Metrics.Flaps)
	}
	if rs.Metrics.FlapRate != 0.8 {
		t.Fatalf("flap rate %v", rs.Metrics.FlapRate)
	}
	if rs.Recommendation != model.RecTune {
		t.Fatalf("rec %s reasons %v", rs.Recommendation, rs.Reasons)
	}
	if !strings.Contains(rs.Reasons[0], "re-fired within 15m") || !strings.Contains(rs.Reasons[0], "currently 30s") {
		t.Fatalf("reason %q", rs.Reasons[0])
	}
	if rs.Rule == nil || rs.Rule.Expr != "up == 0" {
		t.Fatalf("rule %+v", rs.Rule)
	}
	if rs.Team != "platform" {
		t.Fatalf("team %s", rs.Team)
	}
}

func TestPercentiles(t *testing.T) {
	start := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	var events []model.Event
	for i := 1; i <= 5; i++ {
		s := start.Add(time.Duration(i) * time.Hour)
		events = append(events, firing("p"+string(rune('a'+i)), "Slow", s, s.Add(time.Duration(i)*time.Minute), map[string]string{
			"alertname": "Slow", "severity": "page", "team": "a",
		}))
	}
	rep := Evaluate(testCfg(), events, rules.Empty(), week())
	m := rep.Rules[0].Metrics
	if m.TTRSamples != 5 || m.MedianTTRSeconds != 180 || m.P90TTRSeconds != 276 {
		t.Fatalf("ttr samples %d median %v p90 %v", m.TTRSamples, m.MedianTTRSeconds, m.P90TTRSeconds)
	}
}

func TestMuteSLOKeepAndBurn(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	var events []model.Event
	for i := 0; i < 5; i++ {
		s := start.Add(time.Duration(i) * 3 * time.Hour)
		ev := firing("mute"+string(rune('a'+i)), "PodRestarted", s, s.Add(90*time.Second), map[string]string{
			"alertname": "PodRestarted", "severity": "warning", "team": "platform",
		})
		ev.AckKnown = true
		events = append(events, ev)
	}
	for i := 0; i < 15; i++ {
		s := start.Add(time.Duration(i) * time.Hour)
		ev := firing("lat", "CheckoutLatencyHigh", s, s.Add(25*time.Minute), map[string]string{
			"alertname": "CheckoutLatencyHigh", "severity": "page", "team": "payments",
		})
		ev.AckKnown = true
		ev.AckedAt = s.Add(4 * time.Minute)
		events = append(events, ev)
	}
	for i := 0; i < 2; i++ {
		s := start.Add(time.Duration(i+1) * 24 * time.Hour)
		ev := firing("api", "APIDown", s, s.Add(40*time.Minute), map[string]string{
			"alertname": "APIDown", "severity": "page", "team": "payments",
		})
		ev.AckKnown = true
		ev.AckedAt = s.Add(2 * time.Minute)
		events = append(events, ev)
	}
	for day := 0; day < 5; day++ {
		for _, hour := range []int{0, 2, 4} {
			s := start.AddDate(0, 0, day).Add(time.Duration(hour)*time.Hour - 5*time.Hour)
			ev := firing(fmt.Sprintf("burn-%d-%d", day, hour), "APIErrorBudgetBurn", s, s.Add(20*time.Minute), map[string]string{
				"alertname": "APIErrorBudgetBurn", "severity": "page", "team": "payments",
			})
			ev.AckKnown = true
			ev.AckedAt = s.Add(2 * time.Minute)
			events = append(events, ev)
		}
	}
	events = append(events, firing("dog", "Watchdog", start, start.Add(time.Minute), map[string]string{
		"alertname": "Watchdog", "severity": "none", "team": "ops",
	}))

	idx := loadRules(t, `
groups:
- name: checkout
  rules:
  - alert: CheckoutLatencyHigh
    expr: histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m])) > 0.4
    for: 2m
  - alert: APIErrorBudgetBurn
    expr: job:slo_burnrate1h > 14.4
    for: 2m
- name: edge
  rules:
  - alert: APIDown
    expr: probe_success == 0
    for: 1m
`)
	cfg := testCfg()
	cfg.Ignore.Alertnames = map[string]struct{}{"Watchdog": {}}
	rep := Evaluate(cfg, events, idx, week())
	got := map[string]string{}
	for _, rs := range rep.Rules {
		got[rs.Alertname] = rs.Recommendation
		if rs.Alertname == "Watchdog" {
			t.Fatal("Watchdog should have been ignored")
		}
	}
	if rep.Summary.Ignored != 1 {
		t.Fatalf("ignored %d", rep.Summary.Ignored)
	}
	want := map[string]string{
		"PodRestarted":        model.RecMute,
		"CheckoutLatencyHigh": model.RecSLO,
		"APIDown":             model.RecKeep,
		"APIErrorBudgetBurn":  model.RecKeep,
	}
	for name, rec := range want {
		if got[name] != rec {
			var reasons []string
			for _, rs := range rep.Rules {
				if rs.Alertname == name {
					reasons = rs.Reasons
				}
			}
			t.Errorf("%s: got %s want %s (%v)", name, got[name], rec, reasons)
		}
	}
	for _, rs := range rep.Rules {
		if rs.Alertname == "CheckoutLatencyHigh" && !strings.Contains(rs.Reasons[0], "burn rate") {
			t.Fatalf("slo reason %q", rs.Reasons[0])
		}
		if rs.Alertname == "PodRestarted" && !strings.Contains(strings.ToLower(rs.Reasons[0]), "acknowled") {
			t.Fatalf("mute reason %q", rs.Reasons[0])
		}
	}
}

func TestDuplicatesOffHoursAndCarriedFlap(t *testing.T) {
	start := time.Date(2026, 2, 24, 15, 0, 0, 0, time.UTC)
	var events []model.Event
	for i := 0; i < 5; i++ {
		events = append(events, firing("edge"+string(rune('a'+i)), "Edge5xxRate", start, start.Add(30*time.Minute), map[string]string{
			"alertname": "Edge5xxRate", "severity": "page", "team": "edge", "instance": string(rune('a' + i)),
		}))
	}
	saturday := time.Date(2026, 2, 28, 15, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		s := saturday.Add(time.Duration(i) * 2 * time.Hour)
		ev := firing("cert", "CertificateExpiring", s, s.Add(30*time.Minute), map[string]string{
			"alertname": "CertificateExpiring", "severity": "page", "team": "security",
		})
		ev.AckKnown = true
		ev.AckedAt = s.Add(5 * time.Minute)
		events = append(events, ev)
	}
	// A resolve just before the window makes the first in-window fire a flap.
	prevStart := week().Start.Add(-10 * time.Minute)
	events = append(events,
		firing("carry", "Flappy", prevStart, week().Start.Add(time.Minute), map[string]string{
			"alertname": "Flappy", "severity": "page", "team": "platform",
		}),
		firing("carry", "Flappy", week().Start.Add(2*time.Minute), week().Start.Add(3*time.Minute), map[string]string{
			"alertname": "Flappy", "severity": "page", "team": "platform",
		}),
	)
	rep := Evaluate(testCfg(), events, rules.Empty(), week())
	by := map[string]model.RuleScore{}
	for _, rs := range rep.Rules {
		by[rs.Alertname] = rs
	}
	edge := by["Edge5xxRate"]
	if edge.Recommendation != model.RecTune || edge.Metrics.DuplicateRate != 1 {
		t.Fatalf("edge rec %s dup %v reasons %v", edge.Recommendation, edge.Metrics.DuplicateRate, edge.Reasons)
	}
	cert := by["CertificateExpiring"]
	if cert.Recommendation != model.RecTune || cert.Metrics.OffHoursRate != 1 {
		t.Fatalf("cert rec %s off %v reasons %v", cert.Recommendation, cert.Metrics.OffHoursRate, cert.Reasons)
	}
	flap := by["Flappy"]
	if flap.Metrics.Fires != 1 || flap.Metrics.Flaps != 1 || flap.Metrics.CarriedIn != 1 {
		t.Fatalf("flappy %+v", flap.Metrics)
	}
	if flap.Recommendation != model.RecKeep {
		t.Fatalf("small sample should stay keep, got %s", flap.Recommendation)
	}
}

func TestWeightAndDisabledHours(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	var events []model.Event
	for i := 0; i < 4; i++ {
		s := start.Add(time.Duration(i) * 2 * time.Minute)
		events = append(events, firing("fp", "InstanceDown", s, s.Add(time.Minute), map[string]string{
			"alertname": "InstanceDown", "severity": "page", "team": "platform",
		}))
	}
	cfg := testCfg()
	cfg.Weights = config.Weights{Flap: 1}
	cfg.BusinessHours.Enabled = false
	rep := Evaluate(cfg, events, rules.Empty(), week())
	rs := rep.Rules[0]
	if rs.Score != 75 {
		t.Fatalf("score %v components %+v", rs.Score, rs.Components)
	}
	for _, c := range rs.Components {
		if c.Name == "off_hours" || c.Name == "volume" {
			t.Fatalf("unexpected component %+v", rs.Components)
		}
	}
	if !rep.Summary.ThresholdExceeded || rep.Summary.NoisyRules != 1 {
		t.Fatalf("summary %+v", rep.Summary)
	}
}

func TestGraceAutoResolve(t *testing.T) {
	start := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC)
	ev := firing("g", "PodRestarted", start, start.Add(time.Minute), map[string]string{
		"alertname": "PodRestarted", "severity": "warning", "team": "platform",
	})
	rep := Evaluate(testCfg(), []model.Event{ev}, rules.Empty(), week())
	m := rep.Rules[0].Metrics
	if m.AutoResolveMethod != "grace" || m.AutoResolve != 1 {
		t.Fatalf("auto %+v", m)
	}
}

func week() model.Window {
	return model.Window{
		Start: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
}

func firing(fp, name string, start, end time.Time, labels map[string]string) model.Event {
	return model.Event{
		Fingerprint: fp,
		Alertname:   name,
		Status:      model.StatusResolved,
		StartsAt:    start,
		EndsAt:      end,
		Labels:      labels,
		Source:      "test",
	}
}

func testCfg() config.Config {
	return config.Config{
		Timezone: "UTC",
		Location: time.UTC,
		BusinessHours: config.BusinessHours{
			Enabled:  true,
			Timezone: "UTC",
			Location: time.UTC,
			Days:     []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
			StartMin: 9 * 60,
			EndMin:   17 * 60,
		},
		Grouping: config.Grouping{Labels: []string{"team"}, Fallback: "unowned"},
		Flap:     config.Flap{Window: 15 * time.Minute},
		Scoring: config.Scoring{
			VolumeCap:                30,
			ActionableAfter:          10 * time.Minute,
			AutoResolveGrace:         5 * time.Minute,
			OffHoursSeverities:       []string{"page", "critical", "high"},
			CountUnspecifiedSeverity: true,
		},
		Weights: config.Weights{Volume: 1, Flap: 2, ShortTTR: 1.5, AutoResolve: 2, OffHours: 1, Duplicate: 1.2},
		Thresholds: config.Thresholds{
			MaxNoiseScore:   60,
			MaxNoisyRules:   0,
			FailOnThreshold: true,
		},
		Recommendations: config.Recommendations{
			MinFires:           5,
			TuneFlapRate:       0.30,
			TuneDuplicateRatio: 0.30,
			TuneOffHoursRate:   0.50,
			MuteAutoResolve:    0.75,
			SLOMinFires:        15,
		},
	}
}

func loadRules(t *testing.T, body string) *rules.Index {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := rules.Load(dir, []string{"rules.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	return idx
}
