package report

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"sync"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

type htmlReporter struct{}

func (htmlReporter) Format() string { return "html" }

func (htmlReporter) Render(w io.Writer, rep model.Report) error {
	tmpl, err := htmlTemplate()
	if err != nil {
		return err
	}
	rules := make([]htmlRule, 0, len(rep.Rules))
	for i, rs := range rep.Rules {
		width := int(rs.Score + 0.5)
		if width < 0 {
			width = 0
		}
		if width > 100 {
			width = 100
		}
		expr := ""
		if rs.Rule != nil {
			expr = strings.TrimSpace(rs.Rule.Expr)
		}
		var examples []string
		for _, labels := range rs.Examples {
			if line := formatLabels(labels); line != "" {
				examples = append(examples, line)
			}
		}
		rules = append(rules, htmlRule{
			RuleScore: rs,
			Anchor:    fmt.Sprintf("rule-%d", i+1),
			Width:     width,
			Location:  ruleLocation(rs.Rule),
			Expr:      expr,
			Examples:  examples,
			Runbook:   runbook(rs),
			Summary:   summaryOf(rs),
		})
	}
	data := htmlData{
		Report:    rep,
		Window:    formatTime(rep.WindowStart, rep.Timezone) + " to " + formatTime(rep.WindowEnd, rep.Timezone),
		Generated: formatTime(rep.GeneratedAt, rep.Timezone),
		Actions:   Actions(rep.Rules),
		Teams:     Teams(rep.Rules),
		Rules:     rules,
		Worst:     worst(rep),
	}
	return tmpl.Execute(w, data)
}

type htmlData struct {
	Report    model.Report
	Window    string
	Generated string
	Actions   []model.RuleScore
	Teams     []Team
	Rules     []htmlRule
	Worst     string
}

type htmlRule struct {
	model.RuleScore
	Anchor   string
	Width    int
	Location string
	Expr     string
	Examples []string
	Runbook  string
	Summary  string
}

func runbook(rs model.RuleScore) string {
	if rs.Rule == nil {
		return ""
	}
	return firstNonEmpty(rs.Rule.Annotations["runbook_url"], rs.Rule.Annotations["runbook"])
}

func summaryOf(rs model.RuleScore) string {
	if rs.Rule == nil {
		return ""
	}
	return rs.Rule.Annotations["summary"]
}

var (
	tmplOnce sync.Once
	tmpl     *template.Template
	tmplErr  error
)

func htmlTemplate() (*template.Template, error) {
	tmplOnce.Do(func() {
		tmpl, tmplErr = template.New("report").Funcs(template.FuncMap{
			"score": scoreText,
			"pct":   pct,
			"ttr":   ttrText,
			"num":   trimFloat,
			"upper1": func(s string) string {
				if s == "" {
					return ""
				}
				return strings.ToUpper(s[:1]) + s[1:]
			},
			"first": func(ss []string) string {
				if len(ss) == 0 {
					return ""
				}
				return ss[0]
			},
		}).Parse(htmlPage)
	})
	return tmpl, tmplErr
}

const htmlPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Alert hygiene report</title>
<style>
  :root { color-scheme: light; }
  body { margin: 0; background: #f4f5f7; color: #1c1e21; font: 15px/1.5 "Segoe UI", system-ui, sans-serif; }
  main { max-width: 980px; margin: 0 auto; padding: 32px 20px 64px; }
  h1 { font-size: 28px; font-weight: 650; margin: 0 0 8px; }
  h2 { font-size: 18px; margin: 32px 0 12px; }
  h3 { font-size: 16px; margin: 0; }
  p, li { margin: 0 0 8px; }
  .muted { color: #5c6570; }
  .banner { background: #fff; border: 1px solid #e1e4e8; border-left: 4px solid #0f766e; padding: 12px 14px; margin: 16px 0; }
  .banner.bad { border-left-color: #9f1239; }
  .grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
  .card { background: #fff; border: 1px solid #e1e4e8; padding: 12px 14px; }
  .card b { display: block; font-size: 22px; font-weight: 650; }
  table { width: 100%; border-collapse: collapse; background: #fff; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #e6e8eb; vertical-align: top; }
  th { font-size: 12px; letter-spacing: 0.02em; text-transform: uppercase; color: #5c6570; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  .rule { background: #fff; border: 1px solid #e1e4e8; padding: 16px; margin: 12px 0; }
  .bar { height: 8px; background: #e6e8eb; width: 120px; display: inline-block; vertical-align: middle; }
  .bar > span { display: block; height: 8px; background: #0f766e; }
  .tune .bar > span, .tag.tune { background: #9a3412; }
  .mute .bar > span { background: #1e3a8a; }
  .slo .bar > span { background: #065f46; }
  .keep .bar > span { background: #3f3f46; }
  .tag { display: inline-block; color: #fff; font-size: 12px; padding: 1px 6px; }
  .tag.tune { background: #9a3412; }
  .tag.mute { background: #1e3a8a; }
  .tag.slo { background: #065f46; }
  .tag.keep { background: #3f3f46; }
  code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; }
  pre { background: #f6f8fa; padding: 10px 12px; overflow: auto; }
  a { color: #1d4e89; }
  @media (max-width: 720px) { .grid { grid-template-columns: 1fr 1fr; } }
</style>
</head>
<body>
<main>
  <h1>Alert hygiene report</h1>
  <p class="muted">Window {{.Window}}. Generated {{.Generated}}.</p>
  {{if .Report.Summary.ThresholdExceeded}}
  <div class="banner bad">The noise threshold is exceeded: {{.Report.Summary.NoisyRules}} rule(s) at or above {{score .Report.Summary.MaxNoiseScore}}, and {{.Report.Summary.MaxNoisyRules}} are allowed.</div>
  {{else}}
  <div class="banner">No rule is at or above the {{score .Report.Summary.MaxNoiseScore}} noise threshold.</div>
  {{end}}

  <div class="grid">
    <div class="card"><b>{{.Report.Summary.Rules}}</b>rules</div>
    <div class="card"><b>{{.Report.Summary.Events}}</b>instances</div>
    <div class="card"><b>{{.Report.Summary.NoisyRules}}</b>noisy</div>
    <div class="card"><b>{{score .Report.Summary.WorstScore}}</b>{{.Worst}}</div>
  </div>

  <h2>Do this week</h2>
  {{if .Actions}}
  <ol>
    {{range .Actions}}
    <li><span class="tag {{.Recommendation}}">{{upper1 .Recommendation}}</span> <strong>{{.Alertname}}</strong> ({{.Team}}, score {{score .Score}}). {{first .Reasons}}</li>
    {{end}}
  </ol>
  {{else}}
  <p>No rule crossed a tune, mute, or SLO cutoff in this window.</p>
  {{end}}

  <h2>Rules</h2>
  <table>
    <thead>
      <tr><th>Rule</th><th>Team</th><th class="num">Score</th><th>Action</th><th class="num">Fires</th><th class="num">Flap</th></tr>
    </thead>
    <tbody>
      {{range .Rules}}
      <tr>
        <td><a href="#{{.Anchor}}">{{.Alertname}}</a></td>
        <td>{{.Team}}</td>
        <td class="num">{{score .Score}}</td>
        <td><span class="tag {{.Recommendation}}">{{.Recommendation}}</span></td>
        <td class="num">{{.Metrics.Fires}}</td>
        <td class="num">{{pct .Metrics.FlapRate}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  {{range .Rules}}
  <section class="rule {{.Recommendation}}" id="{{.Anchor}}">
    <h3>{{.Alertname}}</h3>
    <p><span class="tag {{.Recommendation}}">{{upper1 .Recommendation}}</span>
      score {{score .Score}}
      <span class="bar"><span style="width: {{.Width}}%"></span></span>
      {{if .Location}}<span class="muted">{{.Location}}</span>{{end}}
    </p>
    {{if .Summary}}<p>{{.Summary}}</p>{{end}}
    <ul>
      {{range .Reasons}}<li>{{.}}</li>{{end}}
    </ul>
    {{if .Expr}}<pre>{{.Expr}}</pre>{{end}}
    <table>
      <tbody>
        <tr><th>Fires</th><td>{{.Metrics.Fires}}</td><th>Flaps</th><td>{{.Metrics.Flaps}} ({{pct .Metrics.FlapRate}})</td></tr>
        <tr><th>Median resolve</th><td>{{ttr .Metrics.TTRSamples .Metrics.MedianTTRSeconds}}</td><th>p90 resolve</th><td>{{ttr .Metrics.TTRSamples .Metrics.P90TTRSeconds}}</td></tr>
        <tr><th>Auto-resolve</th><td>{{pct .Metrics.AutoResolve}} ({{.Metrics.AutoResolveMethod}})</td><th>Off-hours pages</th><td>{{.Metrics.OffHoursPages}} ({{pct .Metrics.OffHoursRate}})</td></tr>
        <tr><th>Overlapping</th><td>{{.Metrics.Duplicates}} ({{pct .Metrics.DuplicateRate}})</td><th>Still firing</th><td>{{.Metrics.StillFiring}}</td></tr>
      </tbody>
    </table>
    {{if .Components}}
    <table>
      <thead><tr><th>Component</th><th class="num">Weight</th><th class="num">Value</th><th class="num">Points</th></tr></thead>
      <tbody>
        {{range .Components}}
        <tr><td>{{.Name}}</td><td class="num">{{num .Weight}}</td><td class="num">{{num .Value}}</td><td class="num">{{score .Points}}</td></tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
    {{if .Examples}}
    <p class="muted">Examples: {{range .Examples}}{{.}} {{end}}</p>
    {{end}}
    {{if .Runbook}}<p>Runbook: <a href="{{.Runbook}}">{{.Runbook}}</a></p>{{end}}
  </section>
  {{end}}

  <h2>By team</h2>
  {{range .Teams}}
  <h3>{{.Name}}</h3>
  <ul>
    {{range .Rules}}
    <li>{{.Alertname}}: {{.Recommendation}}, score {{score .Score}}, {{.Metrics.Fires}} fires</li>
    {{end}}
  </ul>
  {{end}}

  <h2>How to read this</h2>
  <ul>
    <li><strong>tune</strong> means the rule is pointing at something real, but it is too sensitive, too broad, or paging at the wrong time.</li>
    <li><strong>mute</strong> means most instances clear before a person can act.</li>
    <li><strong>slo</strong> means the signal is a user-facing symptom and should page on burn rate.</li>
    <li><strong>keep</strong> means this window does not justify a change.</li>
    {{range .Report.Notes}}<li>{{.}}</li>{{end}}
  </ul>
</main>
</body>
</html>
`
