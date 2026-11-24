// Package cli is the noisefloor command line.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/report"
	"github.com/gaurangomkar/noisefloor/internal/run"
	"github.com/gaurangomkar/noisefloor/internal/source/sqlite"
	"github.com/gaurangomkar/noisefloor/internal/source/webhook"
)

// Version is set from main, usually by -ldflags at release time.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

const usage = `noisefloor scores Alertmanager history and writes an alert-hygiene report.

Usage:
  noisefloor report --config noisefloor.yaml
  noisefloor webhook --config noisefloor.yaml
  noisefloor version

Run noisefloor <command> -h for the flags.
`

const (
	exitOK        = 0
	exitError     = 1
	exitThreshold = 2
)

// Run executes one invocation. args are the arguments after the program name.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitError
	}
	switch args[0] {
	case "report":
		return reportCmd(args[1:], stdout, stderr)
	case "webhook":
		return webhookCmd(args[1:], stdout, stderr)
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, versionLine())
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitError
	}
}

func reportCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		configPath string
		output     string
		formats    string
		failRaw    string
		nowRaw     string
		toStdout   bool
		verbose    bool
	)
	fs.StringVar(&configPath, "config", "", "path to noisefloor.yaml (default: ./noisefloor.yaml)")
	fs.StringVar(&output, "output", "", "directory for the report (default: output.dir from the config)")
	fs.StringVar(&formats, "formats", "", "comma-separated formats: markdown, html, json")
	fs.StringVar(&failRaw, "fail-on-threshold", "", "exit 2 when the noise threshold is exceeded (true or false)")
	fs.StringVar(&nowRaw, "now", "", "RFC3339 end of the window, for a backfill")
	fs.BoolVar(&toStdout, "stdout", false, "also write the markdown report to stdout")
	fs.BoolVar(&verbose, "verbose", false, "log source counts")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: noisefloor report [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	path, err := findConfig(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if nowRaw != "" {
		t, err := time.Parse(time.RFC3339, nowRaw)
		if err != nil {
			fmt.Fprintf(stderr, "now: %v\n", err)
			return exitError
		}
		cfg.Now = t.UTC()
	}
	if output != "" {
		cfg.Output.Dir = output
	}
	if formats != "" {
		cfg.Output.Formats = splitFormats(formats)
	}
	fail := cfg.Thresholds.FailOnThreshold
	if failRaw != "" {
		fail, err = parseBool(failRaw)
		if err != nil {
			fmt.Fprintf(stderr, "fail-on-threshold: %v\n", err)
			return exitError
		}
	}
	logger := newLogger(stderr, verbose)
	rep, err := run.Generate(context.Background(), cfg, logger, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	paths, err := report.Write(cfg.Output.Dir, cfg.Output.Formats, report.Names{
		Markdown: cfg.Output.Markdown,
		HTML:     cfg.Output.HTML,
		JSON:     cfg.Output.JSON,
	}, rep)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if toStdout {
		if err := (reportBy("markdown")).Render(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
	}
	fmt.Fprintf(stderr, "wrote %s\n", strings.Join(paths, ", "))
	fmt.Fprintf(stderr, "%s\n", summaryLine(rep))
	if fail && rep.ExceedsThreshold() {
		fmt.Fprintf(stderr, "noise threshold exceeded\n")
		return exitThreshold
	}
	return exitOK
}

func webhookCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("webhook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		configPath string
		addr       string
		verbose    bool
	)
	fs.StringVar(&configPath, "config", "", "path to noisefloor.yaml (default: ./noisefloor.yaml)")
	fs.StringVar(&addr, "addr", "", "listen address (default: webhook.addr from the config)")
	fs.BoolVar(&verbose, "verbose", false, "log each stored batch at debug as well as info")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: noisefloor webhook [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	path, err := findConfig(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if cfg.Sources.SQLite == nil || cfg.Sources.SQLite.Path == "" {
		fmt.Fprintln(stderr, "webhook requires sources.sqlite.path")
		return exitError
	}
	if addr == "" {
		addr = cfg.Webhook.Addr
	}
	store, err := sqlite.Open(cfg.Sources.SQLite.Path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	defer store.Close()
	logger := newLogger(stderr, verbose)
	if cfg.Webhook.Token == "" {
		logger.Warn("webhook has no token; set webhook.token or NOISEFLOOR_WEBHOOK_TOKEN before exposing it")
	}
	srv := &webhook.Server{Store: store, Logger: logger, Token: cfg.Webhook.Token}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("listening", "addr", addr, "db", cfg.Sources.SQLite.Path)
	fmt.Fprintf(stdout, "noisefloor webhook listening on %s\n", addr)
	if err := srv.ListenAndServe(ctx, addr); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	return exitOK
}

func findConfig(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, name := range []string{"noisefloor.yaml", "noisefloor.yml", ".noisefloor.yaml"} {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no config file found; pass --config")
}

func splitFormats(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		r, err := report.ByFormat(part)
		if err != nil {
			out = append(out, strings.TrimSpace(part))
			continue
		}
		if seen[r.Format()] {
			continue
		}
		seen[r.Format()] = true
		out = append(out, r.Format())
	}
	return out
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "yes", "y":
		return true, nil
	case "0", "f", "false", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("want true or false, got %q", s)
	}
}

func newLogger(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

func summaryLine(rep model.Report) string {
	if rep.Summary.Rules == 0 {
		return "no rules scored"
	}
	line := fmt.Sprintf("%s score %s is the noisiest rule", rep.Summary.WorstRule, trimScore(rep.Summary.WorstScore))
	if rep.ExceedsThreshold() {
		return line + "; threshold exceeded"
	}
	return line
}

func trimScore(v float64) string {
	return fmt.Sprintf("%.1f", v)
}

type renderer interface {
	Render(io.Writer, model.Report) error
}

func reportBy(format string) renderer {
	r, err := report.ByFormat(format)
	if err != nil {
		return markdownFallback{}
	}
	return r
}

type markdownFallback struct{}

func (markdownFallback) Render(w io.Writer, rep model.Report) error {
	r, err := report.ByFormat("markdown")
	if err != nil {
		return err
	}
	return r.Render(w, rep)
}

func versionLine() string {
	line := "noisefloor " + Version
	if Commit != "" {
		line += " " + Commit
	}
	if Date != "" {
		line += " " + Date
	}
	return line
}
