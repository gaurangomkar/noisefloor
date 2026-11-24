# Extending noisefloor

Two extension points cover the cases that come up: a new place alerts come
from, and a new way to publish the report. The score stays in `internal/score`.
If a change needs a new measurement, add it there and to the report, and add a
unit test that would fail if the measurement drifted.

## Add a source

A source implements `source.Source`:

```go
type Source interface {
    Name() string
    Fetch(ctx context.Context, window model.Window) (Result, error)
}
```

`Fetch` returns instances that overlap `window`, plus short notes that are
printed in the report. It should not score, group, or filter ignored alerts.
`run.Generate` does that once for every source, so a source that pre-filters
will disagree with the others.

PagerDuty is the template. It lives in `internal/source/pagerduty`, maps one
API object to one `model.Event`, and is constructed from `internal/run/sources.go`
only when the config block is present.

### What to set on the event

- `Alertname` and `Labels["alertname"]`. `model.Normalize` fills one from the other.
- `StartsAt`. Events without it are dropped.
- `Status` of `firing` or `resolved`. For a firing alert, leave `EndsAt` zero.
- `Fingerprint` if the upstream system has a stable one. Otherwise leave it empty and `Normalize` hashes the labels.
- `AckKnown` true when the source knows whether a human acknowledged it, even if the answer is no. Set `AckedAt` only when someone did.
- `Source` to a short name (`opsgenie`, `oncall`). Collapse joins these when two sources saw the same firing.
- `Silenced` and `Inhibited` only when the source actually knows. Guessing makes the silence sentence a lie.

Call `model.Normalize(event, window.End)` before returning. It canonicalises status and clears a resolve-timeout `EndsAt`.

### Wire it

1. Add the block to the raw config in `internal/config/decode.go` and to `config.Sources`. Pointer fields mean "omitted" rather than "present and zero", which is how `false` and `0` stay distinguishable from a default. Follow `rawPagerDuty`.
2. Validate the block in `config.validate`. A configured source with no credentials should fail the load, not the HTTP call ten seconds later.
3. Construct it in `openSources` in `internal/run/sources.go`.
4. Document the keys in `examples/noisefloor.yaml` and the configuration table in the README.
5. Test the mapping against `httptest`, including pagination if the API has it, and a missing-token error. Do not call the real API from `go test`.

An Opsgenie source would look like this in outline:

```go
type Client struct {
    Token string
    HTTP  *http.Client
}

func (c Client) Name() string { return "opsgenie" }

func (c Client) Fetch(ctx context.Context, window model.Window) (source.Result, error) {
    // GET /v2/alerts?query=createdAt>window.Start
    // For each alert:
    //   StartsAt = createdAt
    //   EndsAt   = report.ackTime or report.closeTime when status is closed
    //   AckKnown = true
    //   AckedAt  = report.ackTime, or zero when it was never acked
    //   Labels   = alias, team, priority, alertname from the message or alias
    return source.Result{Events: events}, nil
}
```

Keep the HTTP client timeout on the config, and never put the token in an error string.

## Add a reporter

A reporter implements `report.Reporter`:

```go
type Reporter interface {
    Format() string
    Render(w io.Writer, rep model.Report) error
}
```

`Render` receives a finished `model.Report`. It must not call the scorer. HTML
and Markdown both call `report.Actions` and `report.Teams` so the "do this
week" list and the per-team list stay in one order.

Register the format in `ByFormat` in `internal/report/report.go`, and accept the
name in `canonicalFormat` in `internal/config/decode.go` so `output.formats`
can name it. `Names.File` needs a filename. Add a golden or a string assertion
the way `internal/report/report_test.go` checks that an alert name of
`<script>` is escaped.

A Slack reporter that posts only the action list is a better first reporter
than a second full HTML page. The action list is `report.Actions(rep.Rules)`.
The first reason on each rule is the sentence to send.

## Change the score

Put the measurement in `internal/score/score.go` and the sentence in
`internal/score/recommend.go`. Add a case to `internal/score/score_test.go`
with hand-built events and a fixed window, not only a golden file. Golden
files in `testdata/golden` lock the wording of the sample week. They are
updated with:

```bash
UPDATE_GOLDEN=1 go test ./internal/run
```

Read the diff. A one-line wording change that rewrites every rule is usually an
accident in sorting or time formatting.

If the JSON shape changes in a way an existing consumer would misread, bump
`model.ScorerVersion`.

## What not to extend

Do not shell out to `amtool` or `jq`. The binary has to run in the distroless
image, which has neither.

Do not add a second notion of "flap" for one source. If PagerDuty's trigger log
and Alertmanager's resolve disagree, collapse them into one instance and let
the shared definition run. Two flap formulas will produce two reports that
cannot be compared.

Do not page from noisefloor. The tool reviews pages. A recommendation that
opens an incident recreates the problem it is measuring.
