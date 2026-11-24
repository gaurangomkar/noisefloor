// Package alertmanager reads the alerts currently known to an Alertmanager.
// Alertmanager does not keep a long history. The webhook receiver is what
// records a week of firings; this client fills in the alerts that are active
// now, including ones that are silenced or inhibited.
package alertmanager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/source"
	"github.com/gaurangomkar/noisefloor/internal/source/amjson"
)

// Client fetches /api/v2/alerts.
type Client struct {
	BaseURL          string
	Username         string
	Password         string
	BearerToken      string
	IncludeSilenced  bool
	IncludeInhibited bool
	HTTP             *http.Client
	Now              func() time.Time
}

// Name implements source.Source.
func (c Client) Name() string { return "alertmanager" }

// Fetch implements source.Source.
func (c Client) Fetch(ctx context.Context, window model.Window) (source.Result, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return source.Result{}, fmt.Errorf("alertmanager url is empty")
	}
	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/") + "/api/v2/alerts")
	if err != nil {
		return source.Result{}, fmt.Errorf("alertmanager url: %w", err)
	}
	q := u.Query()
	q.Set("active", "true")
	q.Set("silenced", strconv.FormatBool(c.IncludeSilenced))
	q.Set("inhibited", strconv.FormatBool(c.IncludeInhibited))
	q.Set("unprocessed", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return source.Result{}, err
	}
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	} else if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "noisefloor")

	resp, err := c.http().Do(req)
	if err != nil {
		return source.Result{}, fmt.Errorf("alertmanager: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, amjson.MaxBytes+1))
	if err != nil {
		return source.Result{}, fmt.Errorf("alertmanager: read body: %w", err)
	}
	if len(body) > amjson.MaxBytes {
		return source.Result{}, fmt.Errorf("alertmanager: response exceeds %d bytes", amjson.MaxBytes)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return source.Result{}, fmt.Errorf("alertmanager: %s: %s", resp.Status, snippet)
	}
	now := window.End
	if c.Now != nil {
		now = c.Now()
	}
	if now.IsZero() {
		now = time.Now()
	}
	events, err := amjson.Parse(body, now)
	if err != nil {
		return source.Result{}, fmt.Errorf("alertmanager: %w", err)
	}
	kept := make([]model.Event, 0, len(events))
	for _, ev := range events {
		ev.Source = "alertmanager"
		if ev.Overlaps(window) {
			kept = append(kept, ev)
		}
	}
	return source.Result{Events: kept}, nil
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
