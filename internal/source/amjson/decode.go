// Package amjson parses Alertmanager notifications, API payloads, and the
// NDJSON export this tool writes and reads.
package amjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

// MaxBytes is the largest payload a file or HTTP body may contain.
const MaxBytes = 512 << 20

// Parse decodes a JSON array, one JSON object, or NDJSON. Objects with an
// alerts field are treated as Alertmanager webhook messages. Everything else
// is one alert.
func Parse(data []byte, now time.Time) ([]model.Event, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var out []model.Event
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		events, err := parseValue(raw, now)
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
	}
	return out, nil
}

func parseValue(raw json.RawMessage, now time.Time) ([]model.Event, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("decode array: %w", err)
		}
		var out []model.Event
		for i, item := range items {
			events, err := parseOne(item, now)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			out = append(out, events...)
		}
		return out, nil
	}
	return parseOne(raw, now)
}

func parseOne(raw json.RawMessage, now time.Time) ([]model.Event, error) {
	var probe struct {
		Alerts json.RawMessage `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(probe.Alerts)) > 0 && !bytes.Equal(bytes.TrimSpace(probe.Alerts), []byte("null")) {
		return parseWebhook(raw, now)
	}
	ev, err := parseEvent(raw, now)
	if err != nil {
		return nil, err
	}
	return []model.Event{ev}, nil
}

type webhookDTO struct {
	Receiver string            `json:"receiver"`
	Alerts   []json.RawMessage `json:"alerts"`
}

func parseWebhook(raw json.RawMessage, now time.Time) ([]model.Event, error) {
	var wh webhookDTO
	if err := json.Unmarshal(raw, &wh); err != nil {
		return nil, err
	}
	var out []model.Event
	for i, item := range wh.Alerts {
		ev, err := parseEvent(item, now)
		if err != nil {
			return nil, fmt.Errorf("alerts[%d]: %w", i, err)
		}
		if ev.Receiver == "" {
			ev.Receiver = wh.Receiver
		}
		if ev.Source == "" {
			ev.Source = "webhook"
		}
		out = append(out, ev)
	}
	return out, nil
}

type alertDTO struct {
	Fingerprint   string            `json:"fingerprint"`
	Alertname     string            `json:"alertname"`
	Status        json.RawMessage   `json:"status"`
	StartsAt      string            `json:"startsAt"`
	EndsAt        string            `json:"endsAt"`
	StartsAtSnake string            `json:"starts_at"`
	EndsAtSnake   string            `json:"ends_at"`
	Labels        map[string]string `json:"labels"`
	Annotations   map[string]string `json:"annotations"`
	GeneratorURL  string            `json:"generatorURL"`
	GeneratorAlt  string            `json:"generator_url"`
	Receiver      string            `json:"receiver"`
	Receivers     []struct {
		Name string `json:"name"`
	} `json:"receivers"`
	AckedAt   json.RawMessage `json:"acked_at"`
	Source    string          `json:"source"`
	Silenced  bool            `json:"silenced"`
	Inhibited bool            `json:"inhibited"`
}

func parseEvent(raw json.RawMessage, now time.Time) (model.Event, error) {
	var dto alertDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return model.Event{}, err
	}
	status, silenced, inhibited, err := parseStatus(dto.Status)
	if err != nil {
		return model.Event{}, err
	}
	start, err := parseTime(first(dto.StartsAtSnake, dto.StartsAt))
	if err != nil {
		return model.Event{}, fmt.Errorf("starts_at: %w", err)
	}
	if start.IsZero() {
		return model.Event{}, fmt.Errorf("missing starts_at")
	}
	end, err := parseTime(first(dto.EndsAtSnake, dto.EndsAt))
	if err != nil {
		return model.Event{}, fmt.Errorf("ends_at: %w", err)
	}
	ack, ackKnown, err := parseAck(dto.AckedAt)
	if err != nil {
		return model.Event{}, fmt.Errorf("acked_at: %w", err)
	}
	receiver := dto.Receiver
	if receiver == "" && len(dto.Receivers) > 0 {
		receiver = dto.Receivers[0].Name
	}
	ev := model.Normalize(model.Event{
		Fingerprint:  dto.Fingerprint,
		Alertname:    dto.Alertname,
		Status:       status,
		StartsAt:     start,
		EndsAt:       end,
		Labels:       dto.Labels,
		Annotations:  dto.Annotations,
		GeneratorURL: first(dto.GeneratorAlt, dto.GeneratorURL),
		Receiver:     receiver,
		Source:       dto.Source,
		AckedAt:      ack,
		AckKnown:     ackKnown,
		Silenced:     silenced || dto.Silenced,
		Inhibited:    inhibited || dto.Inhibited,
	}, now)
	return ev, nil
}

func parseStatus(raw json.RawMessage) (string, bool, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false, false, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false, false, err
		}
		return canonical(s), false, false, nil
	}
	var obj struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false, false, fmt.Errorf("status: %w", err)
	}
	return canonical(obj.State), len(obj.SilencedBy) > 0, len(obj.InhibitedBy) > 0, nil
}

func canonical(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ""
	case "resolved", "ok":
		return model.StatusResolved
	case "firing", "active", "suppressed", "unprocessed":
		return model.StatusFiring
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func parseAck(raw json.RawMessage) (time.Time, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return time.Time{}, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return time.Time{}, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return time.Time{}, false, err
	}
	t, err := parseTime(s)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", s)
}

func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
