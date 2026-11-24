package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReportExitCodes(t *testing.T) {
	root := repoRoot(t)
	cfg := filepath.Join(root, "examples", "quickstart.yaml")
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"report", "--config", cfg, "--output", dir}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
	for _, name := range []string{"noisefloor.md", "noisefloor.html", "noisefloor.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(stderr.String(), "threshold exceeded") {
		t.Fatalf("stderr %s", stderr.String())
	}

	dir = t.TempDir()
	stderr.Reset()
	code = Run([]string{"report", "--config", cfg, "--output", dir, "--fail-on-threshold=false"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
}

func TestVersionAndUsage(t *testing.T) {
	Version = "test"
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "noisefloor test") {
		t.Fatalf("code %d out %q", code, stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "noisefloor report") {
		t.Fatalf("help %d %q", code, stdout.String())
	}
	if code := Run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("empty args %d", code)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}
