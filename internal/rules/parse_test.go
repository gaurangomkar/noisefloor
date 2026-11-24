package rules

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sample = `
groups:
  - name: checkout
    rules:
      - alert: CheckoutLatencyHigh
        expr: histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m])) > 0.4
        for: 2m
        labels:
          severity: page
          team: payments
        annotations:
          summary: Checkout p99 latency is high
      - record: job:http_requests:rate5m
        expr: sum(rate(http_requests_total[5m]))
      - alert: CheckoutLatencyHigh
        expr: histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{job="other"}[5m])) > 1
        for: 10m
        labels:
          severity: page
          team: other
  - name: platform
    rules:
      - alert: InstanceDown
        expr: up == 0
        for: 30s
        keep_firing_for: 5m
        labels:
          severity: page
          team: platform
`

func TestLoadMatchesLabelsAndSkipsRecordingRules(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.Mkdir(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "a.yaml"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Load(dir, []string{"rules"})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Count() != 3 {
		t.Fatalf("count %d", idx.Count())
	}
	if idx.Files() != 1 {
		t.Fatalf("files %d", idx.Files())
	}
	got, ok := idx.Lookup("CheckoutLatencyHigh", map[string]string{"team": "payments", "severity": "page"})
	if !ok {
		t.Fatal("missing rule")
	}
	if got.Group != "checkout" || got.For != 2*time.Minute {
		t.Fatalf("matched %+v", got)
	}
	if got.File != "rules/a.yaml" {
		t.Fatalf("file %s", got.File)
	}
	if got.Line <= 1 {
		t.Fatalf("line %d", got.Line)
	}
	other, ok := idx.Lookup("CheckoutLatencyHigh", map[string]string{"team": "other"})
	if !ok || other.For != 10*time.Minute {
		t.Fatalf("other %+v ok %v", other, ok)
	}
	down, ok := idx.Lookup("InstanceDown", nil)
	if !ok || down.KeepFiringFor != 5*time.Minute {
		t.Fatalf("down %+v", down)
	}
	if _, ok := idx.Lookup("job:http_requests:rate5m", nil); ok {
		t.Fatal("recording rule was indexed")
	}
}

func TestLoadMissingPath(t *testing.T) {
	_, err := Load(t.TempDir(), []string{"nope/*.yaml"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestRejectsRuleWithBothNames(t *testing.T) {
	dir := t.TempDir()
	body := []byte("groups:\n- name: g\n  rules:\n  - alert: A\n    record: a\n    expr: up\n")
	if err := os.WriteFile(filepath.Join(dir, "r.yml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, []string{"r.yml"}); err == nil {
		t.Fatal("expected an error")
	}
}
