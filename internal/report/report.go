// Package report renders a scored week as Markdown, HTML, or JSON.
package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "time/tzdata"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

// Reporter writes one format of a report.
type Reporter interface {
	Format() string
	Render(w io.Writer, rep model.Report) error
}

// ByFormat returns the renderer for a config format name.
func ByFormat(format string) (Reporter, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		return markdownReporter{}, nil
	case "html", "htm":
		return htmlReporter{}, nil
	case "json":
		return jsonReporter{}, nil
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
}

// Names are the file names for each format.
type Names struct {
	Markdown string
	HTML     string
	JSON     string
}

// File returns the configured file name for a canonical format.
func (n Names) File(format string) string {
	switch format {
	case "html":
		if n.HTML != "" {
			return n.HTML
		}
		return "noisefloor.html"
	case "json":
		if n.JSON != "" {
			return n.JSON
		}
		return "noisefloor.json"
	default:
		if n.Markdown != "" {
			return n.Markdown
		}
		return "noisefloor.md"
	}
}

// Write renders each format into dir and returns the paths it wrote.
func Write(dir string, formats []string, names Names, rep model.Report) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	var wrote []string
	seen := map[string]bool{}
	for _, format := range formats {
		r, err := ByFormat(format)
		if err != nil {
			return nil, err
		}
		if seen[r.Format()] {
			continue
		}
		seen[r.Format()] = true
		path := filepath.Join(dir, names.File(r.Format()))
		if err := writeOne(path, r, rep); err != nil {
			return nil, err
		}
		wrote = append(wrote, path)
	}
	return wrote, nil
}

func writeOne(path string, r Reporter, rep model.Report) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	err = r.Render(f, rep)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Team is one grouping value and the rules that belong to it, highest score first.
type Team struct {
	Name  string
	Rules []model.RuleScore
}

// Teams groups rules by team. Rules inside a team keep score order.
func Teams(rules []model.RuleScore) []Team {
	index := map[string]int{}
	var out []Team
	for _, rs := range rules {
		name := rs.Team
		if name == "" {
			name = "unowned"
		}
		i, ok := index[name]
		if !ok {
			index[name] = len(out)
			out = append(out, Team{Name: name})
			i = index[name]
		}
		out[i].Rules = append(out[i].Rules, rs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Actions returns the rules that are not a keep, in score order.
func Actions(rules []model.RuleScore) []model.RuleScore {
	var out []model.RuleScore
	for _, rs := range rules {
		if rs.Recommendation != model.RecKeep {
			out = append(out, rs)
		}
	}
	return out
}

func formatTime(t time.Time, tz string) string {
	loc := time.UTC
	if tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}
	return t.In(loc).Format("2006-01-02 15:04 MST")
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

func scoreText(v float64) string { return fmt.Sprintf("%.1f", v) }

func ttrText(samples int, seconds float64) string {
	if samples == 0 {
		return "n/a"
	}
	return time.Duration(seconds * float64(time.Second)).Round(time.Second).String()
}

func ruleLocation(r *model.RuleMatch) string {
	if r == nil {
		return ""
	}
	loc := r.File
	if r.Line > 0 {
		loc = fmt.Sprintf("%s:%d", r.File, r.Line)
	}
	if r.Group != "" {
		loc += " (" + r.Group + ")"
	}
	return loc
}

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if k == "alertname" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}
