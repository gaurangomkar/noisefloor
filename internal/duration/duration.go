// Package duration parses the duration strings used in config and rule files.
// Go's time.ParseDuration does not accept days or weeks. Prometheus does.
package duration

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Parse accepts a Go duration ("90s", "1h30m", "1.5h") or a Prometheus
// duration with d and w units ("7d", "1d12h", "2w"). A day is 24 hours and a
// week is 7 days. The empty string is an error; callers that treat it as zero
// check before calling.
func Parse(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("duration is empty")
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	d, err := parseCompound(s)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", s, err)
	}
	return d, nil
}

func parseCompound(s string) (time.Duration, error) {
	var total time.Duration
	i := 0
	for i < len(s) {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if start == i {
			return 0, fmt.Errorf("expected a number at %q", s[start:])
		}
		n, err := strconv.ParseInt(s[start:i], 10, 64)
		if err != nil {
			return 0, err
		}
		unitStart := i
		for i < len(s) && (s[i] < '0' || s[i] > '9') {
			i++
		}
		if unitStart == i {
			return 0, fmt.Errorf("missing unit after %d", n)
		}
		mult, err := unit(s[unitStart:i])
		if err != nil {
			return 0, err
		}
		total += time.Duration(n) * mult
	}
	return total, nil
}

func unit(s string) (time.Duration, error) {
	switch s {
	case "ns":
		return time.Nanosecond, nil
	case "us", "µs", "μs":
		return time.Microsecond, nil
	case "ms":
		return time.Millisecond, nil
	case "s":
		return time.Second, nil
	case "m":
		return time.Minute, nil
	case "h":
		return time.Hour, nil
	case "d":
		return 24 * time.Hour, nil
	case "w":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q", s)
	}
}
