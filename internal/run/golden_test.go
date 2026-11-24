package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/report"
)

func TestReferenceConfigParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", "noisefloor.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Parse(data, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestQuickstartRecommendations(t *testing.T) {
	rep := quickstartReport(t)
	got := map[string]string{}
	for _, rs := range rep.Rules {
		got[rs.Alertname] = rs.Recommendation
		t.Logf("%s team=%s score=%.2f rec=%s fires=%d flap=%.2f auto=%.2f off=%.2f dup=%.2f",
			rs.Alertname, rs.Team, rs.Score, rs.Recommendation, rs.Metrics.Fires,
			rs.Metrics.FlapRate, rs.Metrics.AutoResolve, rs.Metrics.OffHoursRate, rs.Metrics.DuplicateRate)
	}
	want := map[string]string{
		"InstanceDown":        model.RecTune,
		"PodRestarted":        model.RecMute,
		"CheckoutLatencyHigh": model.RecSLO,
		"Edge5xxRate":         model.RecTune,
		"CertificateExpiring": model.RecTune,
		"APIDown":             model.RecKeep,
		"APIErrorBudgetBurn":  model.RecKeep,
	}
	if len(got) != len(want) {
		t.Fatalf("rules %#v", got)
	}
	for name, rec := range want {
		if got[name] != rec {
			t.Errorf("%s: got %s want %s", name, got[name], rec)
		}
	}
	if !rep.Summary.ThresholdExceeded {
		t.Fatal("expected the sample week to exceed the threshold")
	}
	if _, ok := got["Watchdog"]; ok {
		t.Fatal("Watchdog was not ignored")
	}
}

func TestGoldenReport(t *testing.T) {
	rep := quickstartReport(t)
	root := repoRoot(t)
	check := func(format, name string) {
		t.Helper()
		r, err := report.ByFormat(format)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := r.Render(&buf, rep); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "testdata", "golden", name)
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, buf.Bytes()) {
			t.Fatalf("%s drifted from %s; rerun with UPDATE_GOLDEN=1 and review the diff", format, path)
		}
	}
	check("markdown", "report.md")
	check("json", "report.json")
}

func quickstartReport(t *testing.T) model.Report {
	t.Helper()
	cfg, err := config.Load(filepath.Join(repoRoot(t), "examples", "quickstart.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Generate(context.Background(), cfg, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}
