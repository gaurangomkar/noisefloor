package source

import (
	"sort"
	"strings"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

// Collapse merges notifications that describe the same firing.
// The key is fingerprint, alert name, and start time. A resolved notification
// wins over a firing one, the earliest acknowledgement is kept, and silence
// or inhibition sticks once seen. Events with no start time are counted as
// dropped and omitted.
func Collapse(events []model.Event) (kept []model.Event, dropped int) {
	type acc struct {
		ev      model.Event
		sources map[string]struct{}
	}
	order := make([]string, 0, len(events))
	by := make(map[string]*acc, len(events))
	for _, e := range events {
		if e.StartsAt.IsZero() {
			dropped++
			continue
		}
		if e.Alertname == "" {
			e.Alertname = "(unnamed)"
		}
		key := e.Fingerprint + "\x00" + e.Alertname + "\x00" + e.StartsAt.UTC().Format(time.RFC3339Nano)
		cur := by[key]
		if cur == nil {
			cur = &acc{ev: e, sources: map[string]struct{}{}}
			by[key] = cur
			order = append(order, key)
		} else {
			cur.ev = mergeEvent(cur.ev, e)
		}
		if e.Source != "" {
			cur.sources[e.Source] = struct{}{}
		}
	}
	kept = make([]model.Event, 0, len(order))
	for _, key := range order {
		ev := by[key].ev
		ev.Source = joinSources(by[key].sources)
		kept = append(kept, ev)
	}
	sort.Slice(kept, func(i, j int) bool {
		if !kept[i].StartsAt.Equal(kept[j].StartsAt) {
			return kept[i].StartsAt.Before(kept[j].StartsAt)
		}
		if kept[i].Alertname != kept[j].Alertname {
			return kept[i].Alertname < kept[j].Alertname
		}
		return kept[i].Fingerprint < kept[j].Fingerprint
	})
	return kept, dropped
}

func mergeEvent(a, b model.Event) model.Event {
	if b.Status == model.StatusResolved && (a.Status != model.StatusResolved || a.EndsAt.IsZero() || (!b.EndsAt.IsZero() && b.EndsAt.After(a.EndsAt))) {
		a.Status = model.StatusResolved
		if !b.EndsAt.IsZero() {
			a.EndsAt = b.EndsAt
		}
	}
	if a.GeneratorURL == "" {
		a.GeneratorURL = b.GeneratorURL
	}
	if a.Receiver == "" {
		a.Receiver = b.Receiver
	}
	a.Labels = fillMap(a.Labels, b.Labels)
	a.Annotations = fillMap(a.Annotations, b.Annotations)
	a.Silenced = a.Silenced || b.Silenced
	a.Inhibited = a.Inhibited || b.Inhibited
	if b.AckKnown {
		a.AckKnown = true
		if !b.AckedAt.IsZero() && (a.AckedAt.IsZero() || b.AckedAt.Before(a.AckedAt)) {
			a.AckedAt = b.AckedAt
		}
	}
	return a
}

func fillMap(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
	return dst
}

func joinSources(set map[string]struct{}) string {
	if len(set) == 0 {
		return ""
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
