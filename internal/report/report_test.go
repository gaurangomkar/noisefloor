package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

func TestRenderEscapesAndSummarizes(t *testing.T) {
	rep := sampleReport()
	var md, html, js bytes.Buffer
	if err := (markdownReporter{}).Render(&md, rep); err != nil {
		t.Fatal(err)
	}
	if err := (htmlReporter{}).Render(&html, rep); err != nil {
		t.Fatal(err)
	}
	if err := (jsonReporter{}).Render(&js, rep); err != nil {
		t.Fatal(err)
	}
	text := md.String()
	for _, want := range []string{"Do this week", "Tune <script>", "re-fired within 15m", "By team", "platform"} {
		if !strings.Contains(text, want) {
			t.Fatalf("markdown missing %q\n%s", want, text)
		}
	}
	page := html.String()
	if strings.Contains(page, "<script>") {
		t.Fatalf("html did not escape the alert name:\n%s", page)
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("expected an escaped alert name")
	}
	if !strings.Contains(page, "Do this week") || !strings.Contains(page, "up == 0") {
		t.Fatalf("html missing sections:\n%s", page)
	}
	var decoded model.Report
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Rules[0].Alertname != "<script>" || decoded.Summary.ThresholdExceeded != true {
		t.Fatalf("%+v", decoded.Summary)
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	paths, err := Write(dir, []string{"md", "html", "json"}, Names{
		Markdown: "noisefloor.md",
		HTML:     "noisefloor.html",
		JSON:     "noisefloor.json",
	}, sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths %v", paths)
	}
}

func sampleReport() model.Report {
	return model.Report{
		ScorerVersion: model.ScorerVersion,
		GeneratedAt:   time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		WindowStart:   time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		WindowEnd:     time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		Timezone:      "UTC",
		Sources:       []model.SourceStat{{Name: "file:alerts.ndjson", Events: 4}},
		Summary: model.Summary{
			Events:            4,
			Rules:             1,
			NoisyRules:        1,
			MaxNoiseScore:     60,
			MaxNoisyRules:     0,
			ThresholdExceeded: true,
			WorstScore:        75,
			WorstRule:         "<script> (platform)",
			Recommendations: map[string]int{
				model.RecTune: 1, model.RecMute: 0, model.RecSLO: 0, model.RecKeep: 0,
			},
			BusinessHours:    "Mon, Tue, Wed, Thu, Fri 09:00-17:00 UTC",
			FlapWindow:       "15m",
			AutoResolveGrace: "5m",
			VolumeCap:        30,
			ActionableAfter:  "10m",
		},
		Rules: []model.RuleScore{{
			Alertname:      "<script>",
			Team:           "platform",
			Group:          map[string]string{"team": "platform"},
			Score:          75,
			Recommendation: model.RecTune,
			Reasons:        []string{"4 of 5 instances re-fired within 15m of resolving."},
			Metrics: model.Metrics{
				Fires: 5, Flaps: 4, FlapRate: 0.8, Resolved: 5,
				MedianTTRSeconds: 60, P90TTRSeconds: 60, TTRSamples: 5,
				AutoResolve: 1, AutoResolveMethod: "grace", AutoResolveSamples: 5,
				Pages: 5,
			},
			Components: []model.Component{{Name: "flap", Weight: 2, Value: 0.8, Points: 40}},
			Rule: &model.RuleMatch{
				File: "rules.yaml", Group: "platform", Line: 4,
				Expr: "up == 0", For: "30s",
				Annotations: map[string]string{"summary": "instance down"},
			},
			Examples: []map[string]string{{"alertname": "<script>", "severity": "page", "team": "platform"}},
		}},
		Notes: []string{"A flap is a firing that starts within 15m after the previous instance resolved."},
	}
}
