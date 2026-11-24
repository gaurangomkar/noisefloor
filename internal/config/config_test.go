package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDefaultsAndPartialWeights(t *testing.T) {
	cfg, err := Parse([]byte(`
window: 7d
sources:
  files:
    - path: alerts.ndjson
weights:
  flap: 0
thresholds:
  fail_on_threshold: false
`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Window != 7*24*time.Hour {
		t.Fatalf("window %s", cfg.Window)
	}
	if cfg.Weights.Flap != 0 {
		t.Fatalf("flap weight %v", cfg.Weights.Flap)
	}
	if cfg.Weights.Volume != 1 {
		t.Fatalf("volume weight was overwritten: %v", cfg.Weights.Volume)
	}
	if cfg.Thresholds.FailOnThreshold {
		t.Fatal("fail_on_threshold should be false")
	}
	if cfg.Thresholds.MaxNoiseScore != 60 {
		t.Fatalf("max noise %v", cfg.Thresholds.MaxNoiseScore)
	}
	if cfg.Flap.Window != 15*time.Minute {
		t.Fatalf("flap window %s", cfg.Flap.Window)
	}
	if !cfg.Scoring.CountUnspecifiedSeverity {
		t.Fatal("expected unspecified severity to count by default")
	}
	if got := cfg.Grouping.Labels; len(got) != 1 || got[0] != "team" {
		t.Fatalf("grouping %v", got)
	}
}

func TestParseRequiresSource(t *testing.T) {
	_, err := Parse([]byte("window: 24h\n"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "at least one source") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownField(t *testing.T) {
	_, err := Parse([]byte("window: 24h\nnot_a_key: true\n"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not_a_key") {
		t.Fatalf("err = %v", err)
	}
}

func TestBusinessHoursAndOffSwitch(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Parse([]byte(`
sources:
  files:
    - path: a.ndjson
business_hours:
  timezone: America/New_York
  days: [mon, tue, wed, thu, fri]
  start: "09:00"
  end: "17:00"
`), dir)
	if err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC) // 10:00 EST
	if !cfg.BusinessHours.Contains(monday) {
		t.Fatal("expected Monday 10:00 EST to be in hours")
	}
	saturday := time.Date(2026, 2, 28, 15, 0, 0, 0, time.UTC)
	if cfg.BusinessHours.Contains(saturday) {
		t.Fatal("Saturday should be off hours")
	}
	late := time.Date(2026, 2, 23, 23, 0, 0, 0, time.UTC) // 18:00 EST
	if cfg.BusinessHours.Contains(late) {
		t.Fatal("18:00 EST should be outside 09:00-17:00")
	}

	off, err := Parse([]byte(`
sources:
  files:
    - path: a.ndjson
business_hours:
  enabled: false
`), dir)
	if err != nil {
		t.Fatal(err)
	}
	if off.BusinessHours.Enabled {
		t.Fatal("business hours should be disabled")
	}
}

func TestRejectsInvertedHours(t *testing.T) {
	_, err := Parse([]byte(`
sources:
  files:
    - path: a.ndjson
business_hours:
  start: "18:00"
  end: "09:00"
`), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "cross midnight") {
		t.Fatalf("err = %v", err)
	}
}

func TestPagerDutyTokenFromEnv(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "pd-secret")
	cfg, err := Parse([]byte(`
sources:
  pagerduty: {}
`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources.PagerDuty == nil || cfg.Sources.PagerDuty.Token != "pd-secret" {
		t.Fatalf("token %+v", cfg.Sources.PagerDuty)
	}
	if cfg.Sources.PagerDuty.BaseURL != "https://api.pagerduty.com" {
		t.Fatalf("base %s", cfg.Sources.PagerDuty.BaseURL)
	}
}

func TestMissingPagerDutyToken(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "")
	_, err := Parse([]byte("sources:\n  pagerduty: {}\n"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v", err)
	}
}

func TestRelativePathsAndFormats(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Parse([]byte(`
sources:
  files:
    - path: exports/alerts.ndjson
  sqlite:
    path: state/noisefloor.db
rules:
  paths:
    - rules
output:
  dir: out
  formats: [md, json, md]
`), dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources.Files[0].Path != filepath.Join(dir, "exports", "alerts.ndjson") {
		t.Fatalf("file path %s", cfg.Sources.Files[0].Path)
	}
	if cfg.Sources.Files[0].Name != "file:exports/alerts.ndjson" {
		t.Fatalf("name %s", cfg.Sources.Files[0].Name)
	}
	if cfg.Sources.SQLite.Path != filepath.Join(dir, "state", "noisefloor.db") {
		t.Fatalf("sqlite %s", cfg.Sources.SQLite.Path)
	}
	if strings.Join(cfg.Output.Formats, ",") != "markdown,json" {
		t.Fatalf("formats %v", cfg.Output.Formats)
	}
	if cfg.Output.Dir != filepath.Join(dir, "out") {
		t.Fatalf("dir %s", cfg.Output.Dir)
	}
}

func TestIgnoreAndBadPattern(t *testing.T) {
	cfg, err := Parse([]byte(`
sources:
  files:
    - path: a.ndjson
ignore:
  alertnames: [Watchdog]
  name_patterns: ['(?i)^info']
  matchers:
    - labels:
        severity: none
`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Ignore.Skip("Watchdog", nil) {
		t.Fatal("expected Watchdog to be ignored")
	}
	if !cfg.Ignore.Skip("InfoInhibitor", nil) {
		t.Fatal("expected pattern match")
	}
	if !cfg.Ignore.Skip("Other", map[string]string{"severity": "none"}) {
		t.Fatal("expected matcher")
	}
	if cfg.Ignore.Skip("Other", map[string]string{"severity": "page"}) {
		t.Fatal("matcher should not match a page")
	}

	_, err = Parse([]byte("sources:\n  files:\n    - path: a.ndjson\nignore:\n  name_patterns: ['[']\n"), t.TempDir())
	if err == nil {
		t.Fatal("expected a bad regex to fail")
	}
}

func TestBearerTokenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("sekret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse([]byte(`
sources:
  alertmanager:
    url: http://localhost:9093
    bearer_token_file: token
`), dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources.Alertmanager.BearerToken != "sekret" {
		t.Fatalf("token %q", cfg.Sources.Alertmanager.BearerToken)
	}
	if !cfg.Sources.Alertmanager.IncludeSilenced {
		t.Fatal("silenced alerts are included by default")
	}
}

func TestLoadPinsNow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noisefloor.yaml")
	body := []byte("now: 2026-03-02T00:00:00Z\nsources:\n  files:\n    - path: a.ndjson\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	if !cfg.EffectiveNow(time.Now()).Equal(want) {
		t.Fatalf("now %s", cfg.Now)
	}
}
