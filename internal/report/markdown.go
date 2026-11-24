package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

type markdownReporter struct{}

func (markdownReporter) Format() string { return "markdown" }

func (markdownReporter) Render(w io.Writer, rep model.Report) error {
	var b strings.Builder
	writeMarkdown(&b, rep)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeMarkdown(b *strings.Builder, rep model.Report) {
	start := formatTime(rep.WindowStart, rep.Timezone)
	end := formatTime(rep.WindowEnd, rep.Timezone)
	fmt.Fprintf(b, "# Alert hygiene report\n\n")
	fmt.Fprintf(b, "Window %s to %s. Generated %s.\n\n", start, end, formatTime(rep.GeneratedAt, rep.Timezone))
	if rep.Summary.ThresholdExceeded {
		fmt.Fprintf(b, "The noise threshold is exceeded: %d rule(s) at or above %.1f, and %d are allowed. `noisefloor report` exits 2.\n\n", rep.Summary.NoisyRules, rep.Summary.MaxNoiseScore, rep.Summary.MaxNoisyRules)
	} else {
		fmt.Fprintf(b, "No rule is at or above the %.1f noise threshold (allowed noisy rules: %d).\n\n", rep.Summary.MaxNoiseScore, rep.Summary.MaxNoisyRules)
	}

	fmt.Fprintf(b, "## Do this week\n\n")
	actions := Actions(rep.Rules)
	if len(actions) == 0 {
		fmt.Fprintf(b, "No rule crossed a tune, mute, or SLO cutoff in this window.\n\n")
	} else {
		for i, rs := range actions {
			reason := ""
			if len(rs.Reasons) > 0 {
				reason = rs.Reasons[0]
			}
			fmt.Fprintf(b, "%d. **%s %s** (%s, score %s). %s\n", i+1, strings.ToUpper(rs.Recommendation[:1])+rs.Recommendation[1:], rs.Alertname, rs.Team, scoreText(rs.Score), reason)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(b, "## Summary\n\n")
	fmt.Fprintf(b, "| | |\n| --- | --- |\n")
	fmt.Fprintf(b, "| Instances scored | %d |\n", rep.Summary.Events)
	fmt.Fprintf(b, "| Ignored | %d |\n", rep.Summary.Ignored)
	fmt.Fprintf(b, "| Rules | %d |\n", rep.Summary.Rules)
	fmt.Fprintf(b, "| Noisy rules | %d |\n", rep.Summary.NoisyRules)
	fmt.Fprintf(b, "| Worst | %s |\n", cell(worst(rep)))
	fmt.Fprintf(b, "| Tune | %d |\n", rep.Summary.Recommendations[model.RecTune])
	fmt.Fprintf(b, "| Mute | %d |\n", rep.Summary.Recommendations[model.RecMute])
	fmt.Fprintf(b, "| SLO burn rate | %d |\n", rep.Summary.Recommendations[model.RecSLO])
	fmt.Fprintf(b, "| Keep | %d |\n", rep.Summary.Recommendations[model.RecKeep])
	b.WriteString("\n")

	if len(rep.Sources) > 0 {
		fmt.Fprintf(b, "Sources, before dedupe:\n\n")
		for _, s := range rep.Sources {
			fmt.Fprintf(b, "- %s: %d\n", s.Name, s.Events)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(b, "## Rules\n\n")
	if len(rep.Rules) == 0 {
		fmt.Fprintf(b, "No alert instances started during this window.\n\n")
	} else {
		fmt.Fprintf(b, "| Rule | Team | Score | Action | Fires | Flap | Auto-resolve | Off-hours | Overlap |\n")
		fmt.Fprintf(b, "| --- | --- | ---: | --- | ---: | ---: | ---: | ---: | ---: |\n")
		for _, rs := range rep.Rules {
			fmt.Fprintf(b, "| %s | %s | %s | %s | %d | %s | %s | %s | %s |\n",
				cell(rs.Alertname), cell(rs.Team), scoreText(rs.Score), rs.Recommendation,
				rs.Metrics.Fires, pct(rs.Metrics.FlapRate), pct(rs.Metrics.AutoResolve),
				pct(rs.Metrics.OffHoursRate), pct(rs.Metrics.DuplicateRate))
		}
		b.WriteString("\n")
		for _, rs := range rep.Rules {
			writeRule(b, rs)
		}
	}

	fmt.Fprintf(b, "## By team\n\n")
	if len(rep.Rules) == 0 {
		fmt.Fprintf(b, "No teams to group.\n\n")
	}
	for _, team := range Teams(rep.Rules) {
		fmt.Fprintf(b, "### %s\n\n", team.Name)
		for _, rs := range team.Rules {
			fmt.Fprintf(b, "- %s: %s, score %s, %d fires\n", rs.Alertname, rs.Recommendation, scoreText(rs.Score), rs.Metrics.Fires)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(b, "## How to read this\n\n")
	fmt.Fprintf(b, "- **tune** means the rule is pointing at something real, but it is too sensitive, too broad, or paging at the wrong time.\n")
	fmt.Fprintf(b, "- **mute** means most instances clear before a person can act. Stop paging. A ticket or a dashboard is enough, or delete the rule.\n")
	fmt.Fprintf(b, "- **slo** means the signal is a user-facing symptom. Page on a multi-window burn rate and keep the single threshold as a ticket.\n")
	fmt.Fprintf(b, "- **keep** means this window does not justify a change.\n\n")
	for _, note := range rep.Notes {
		fmt.Fprintf(b, "- %s\n", note)
	}
	if len(rep.Notes) > 0 {
		b.WriteString("\n")
	}
}

func worst(rep model.Report) string {
	if rep.Summary.WorstRule == "" {
		return "n/a"
	}
	return fmt.Sprintf("%s at %s", rep.Summary.WorstRule, scoreText(rep.Summary.WorstScore))
}

func writeRule(b *strings.Builder, rs model.RuleScore) {
	fmt.Fprintf(b, "### %s\n\n", rs.Alertname)
	fmt.Fprintf(b, "| | |\n| --- | --- |\n")
	fmt.Fprintf(b, "| Team | %s |\n", cell(rs.Team))
	fmt.Fprintf(b, "| Score | %s |\n", scoreText(rs.Score))
	fmt.Fprintf(b, "| Recommendation | %s |\n", rs.Recommendation)
	if loc := ruleLocation(rs.Rule); loc != "" {
		fmt.Fprintf(b, "| Rule | %s |\n", cell(loc))
	}
	if rs.Rule != nil && rs.Rule.For != "" {
		fmt.Fprintf(b, "| For | %s |\n", cell(rs.Rule.For))
	}
	if rs.Rule != nil && rs.Rule.KeepFiringFor != "" {
		fmt.Fprintf(b, "| Keep firing for | %s |\n", cell(rs.Rule.KeepFiringFor))
	}
	b.WriteString("\n")
	if rs.Rule != nil && strings.TrimSpace(rs.Rule.Expr) != "" {
		fmt.Fprintf(b, "Expression:\n\n```promql\n%s\n```\n\n", strings.TrimSpace(rs.Rule.Expr))
	}
	if len(rs.Reasons) == 0 {
		fmt.Fprintf(b, "No reasons recorded.\n\n")
	} else {
		for _, reason := range rs.Reasons {
			fmt.Fprintf(b, "- %s\n", reason)
		}
		b.WriteString("\n")
	}
	m := rs.Metrics
	fmt.Fprintf(b, "| Metric | Value |\n| --- | --- |\n")
	fmt.Fprintf(b, "| Fires | %d |\n", m.Fires)
	fmt.Fprintf(b, "| Flaps | %d (%s) |\n", m.Flaps, pct(m.FlapRate))
	fmt.Fprintf(b, "| Median time to resolve | %s |\n", ttrText(m.TTRSamples, m.MedianTTRSeconds))
	fmt.Fprintf(b, "| p90 time to resolve | %s |\n", ttrText(m.TTRSamples, m.P90TTRSeconds))
	fmt.Fprintf(b, "| Auto-resolve | %s (%s, %d samples) |\n", pct(m.AutoResolve), m.AutoResolveMethod, m.AutoResolveSamples)
	fmt.Fprintf(b, "| Pages | %d |\n", m.Pages)
	fmt.Fprintf(b, "| Off-hours pages | %d (%s) |\n", m.OffHoursPages, pct(m.OffHoursRate))
	fmt.Fprintf(b, "| Overlapping instances | %d (%s) |\n", m.Duplicates, pct(m.DuplicateRate))
	fmt.Fprintf(b, "| Still firing | %d |\n", m.StillFiring)
	fmt.Fprintf(b, "| Started before the window | %d |\n", m.CarriedIn)
	fmt.Fprintf(b, "| Silenced | %d |\n", m.Silenced)
	fmt.Fprintf(b, "| Inhibited | %d |\n", m.Inhibited)
	b.WriteString("\n")
	if len(rs.Components) > 0 {
		fmt.Fprintf(b, "| Component | Weight | Value | Points |\n| --- | ---: | ---: | ---: |\n")
		for _, c := range rs.Components {
			fmt.Fprintf(b, "| %s | %s | %s | %s |\n", c.Name, trimFloat(c.Weight), trimFloat(c.Value), scoreText(c.Points))
		}
		b.WriteString("\n")
	}
	if len(rs.Examples) > 0 {
		fmt.Fprintf(b, "Example labels:\n\n")
		for _, labels := range rs.Examples {
			if line := formatLabels(labels); line != "" {
				fmt.Fprintf(b, "- %s\n", line)
			}
		}
		b.WriteString("\n")
	}
	if rs.Rule != nil {
		if summary := rs.Rule.Annotations["summary"]; summary != "" {
			fmt.Fprintf(b, "Summary: %s\n\n", summary)
		}
		if runbook := firstNonEmpty(rs.Rule.Annotations["runbook_url"], rs.Rule.Annotations["runbook"]); runbook != "" {
			fmt.Fprintf(b, "Runbook: %s\n\n", runbook)
		}
	}
}

func trimFloat(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
