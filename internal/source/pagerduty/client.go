// Package pagerduty reads incidents from the PagerDuty REST API.
// It is optional: the source is constructed only when the config enables it.
package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/source"
)

const (
	defaultBase = "https://api.pagerduty.com"
	pageSize    = 100
	maxPages    = 50
)

var (
	statusPrefix = regexp.MustCompile(`(?i)^\[(?:firing|resolved)(?::\d+)?\]\s*`)
	promName     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Client lists incidents in a window.
type Client struct {
	Token      string
	BaseURL    string
	ServiceIDs []string
	HTTP       *http.Client
}

// Name implements source.Source.
func (c Client) Name() string { return "pagerduty" }

// Fetch implements source.Source.
func (c Client) Fetch(ctx context.Context, window model.Window) (source.Result, error) {
	if strings.TrimSpace(c.Token) == "" {
		return source.Result{}, fmt.Errorf("pagerduty token is empty")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultBase
	}
	var (
		out       []model.Event
		offset    int
		truncated bool
	)
	for page := 0; page < maxPages; page++ {
		batch, more, err := c.page(ctx, base, window, offset)
		if err != nil {
			return source.Result{}, err
		}
		out = append(out, batch...)
		if !more || len(batch) == 0 {
			truncated = false
			break
		}
		offset += len(batch)
		if page == maxPages-1 {
			truncated = true
		}
	}
	res := source.Result{Events: out}
	if truncated {
		res.Notes = []string{fmt.Sprintf("pagerduty: stopped after %d incidents", len(out))}
	}
	return res, nil
}

func (c Client) page(ctx context.Context, base string, window model.Window, offset int) ([]model.Event, bool, error) {
	u, err := url.Parse(base + "/incidents")
	if err != nil {
		return nil, false, err
	}
	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", pageSize))
	q.Set("offset", fmt.Sprintf("%d", offset))
	q.Set("since", window.Start.UTC().Format(time.RFC3339))
	q.Set("until", window.End.UTC().Format(time.RFC3339))
	q.Set("sort_by", "created_at:asc")
	q.Add("statuses[]", "triggered")
	q.Add("statuses[]", "acknowledged")
	q.Add("statuses[]", "resolved")
	for _, id := range c.ServiceIDs {
		q.Add("service_ids[]", id)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Token token="+c.Token)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	req.Header.Set("User-Agent", "noisefloor")

	resp, err := c.http().Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("pagerduty: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, false, fmt.Errorf("pagerduty: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, false, fmt.Errorf("pagerduty: %s: %s", resp.Status, snippet)
	}
	var payload struct {
		Incidents []incident `json:"incidents"`
		More      bool       `json:"more"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("pagerduty: %w", err)
	}
	events := make([]model.Event, 0, len(payload.Incidents))
	for _, inc := range payload.Incidents {
		ev, err := inc.event()
		if err != nil {
			return nil, false, fmt.Errorf("pagerduty incident %s: %w", inc.ID, err)
		}
		events = append(events, ev)
	}
	return events, payload.More, nil
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type incident struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	Urgency          string `json:"urgency"`
	CreatedAt        string `json:"created_at"`
	ResolvedAt       string `json:"resolved_at"`
	HTMLURL          string `json:"html_url"`
	Acknowledgements []struct {
		At string `json:"at"`
	} `json:"acknowledgements"`
	Service struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"service"`
}

func (inc incident) event() (model.Event, error) {
	start, err := time.Parse(time.RFC3339, inc.CreatedAt)
	if err != nil {
		return model.Event{}, fmt.Errorf("created_at: %w", err)
	}
	status := model.StatusFiring
	var end time.Time
	if inc.ResolvedAt != "" || strings.EqualFold(inc.Status, "resolved") {
		if inc.ResolvedAt != "" {
			end, err = time.Parse(time.RFC3339, inc.ResolvedAt)
			if err != nil {
				return model.Event{}, fmt.Errorf("resolved_at: %w", err)
			}
		}
		status = model.StatusResolved
	}
	var ack time.Time
	for _, a := range inc.Acknowledgements {
		if a.At == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, a.At)
		if err != nil {
			return model.Event{}, fmt.Errorf("acknowledgement: %w", err)
		}
		if ack.IsZero() || t.Before(ack) {
			ack = t
		}
	}
	name := alertName(inc.Title)
	severity := "page"
	if strings.EqualFold(inc.Urgency, "low") {
		severity = "info"
	}
	labels := map[string]string{
		"alertname":   name,
		"severity":    severity,
		"urgency":     inc.Urgency,
		"service":     inc.Service.Summary,
		"incident_id": inc.ID,
	}
	ev := model.Normalize(model.Event{
		Fingerprint:  "pd:" + inc.ID,
		Alertname:    name,
		Status:       status,
		StartsAt:     start,
		EndsAt:       end,
		Labels:       labels,
		GeneratorURL: inc.HTMLURL,
		Source:       "pagerduty",
		AckedAt:      ack,
		AckKnown:     true,
	}, time.Now())
	return ev, nil
}

// alertName strips an Alertmanager-style [FIRING:n] prefix. When the remainder
// starts with a Prometheus-style identifier, that identifier is the alert name.
// Otherwise the whole title is kept so unrelated incidents are not merged.
func alertName(title string) string {
	title = strings.TrimSpace(statusPrefix.ReplaceAllString(title, ""))
	if title == "" {
		return "(untitled)"
	}
	field := title
	if i := strings.IndexAny(title, " \t"); i > 0 {
		field = title[:i]
	}
	if promName.MatchString(field) {
		return field
	}
	return title
}
