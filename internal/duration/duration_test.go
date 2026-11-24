package duration

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"168h", 168 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"7d", 7 * 24 * time.Hour},
		{"1d12h", 36 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"30s", 30 * time.Second},
		{"500ms", 500 * time.Millisecond},
		{"0s", 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{"", "yesterday", "1", "10x", "d"} {
		if _, err := Parse(in); err == nil {
			t.Fatalf("Parse(%q) succeeded", in)
		}
	}
}
