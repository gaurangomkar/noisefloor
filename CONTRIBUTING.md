# Contributing to noisefloor

Thanks for the interest. noisefloor is a small tool with a narrow job: read alert
history, score it, and say which rules to tune, mute, or turn into SLO burn rates.
Changes that make that report more trustworthy are welcome. Changes that turn it
into a general observability platform are not.

## Development

Requirements: Go 1.22 or newer. SQLite is embedded through `modernc.org/sqlite`,
so CGO is not required.

```bash
git clone https://github.com/gaurangomkar/noisefloor
cd noisefloor
go test ./...
go vet ./...
go build -o noisefloor ./cmd/noisefloor
```

Lint matches CI:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0
golangci-lint run
```

The sample week is pinned, so this should print a report and exit 2:

```bash
go run ./cmd/noisefloor report --config examples/quickstart.yaml --output out
```

Golden files live in `testdata/golden`. After an intentional change to report
wording or scoring, regenerate them and review the diff:

```bash
UPDATE_GOLDEN=1 go test ./internal/run
```

## How the code is shaped

- `internal/source` defines `Source`. Alertmanager, JSON files, SQLite, and PagerDuty implement it.
- `internal/score` is pure: events in, a `model.Report` out. No I/O.
- `internal/report` renders a report. Markdown, HTML, and JSON implement `Reporter`.
- `internal/run` is the composition root the CLI calls.

`docs/extending.md` walks through a new source and a new reporter. `docs/architecture.md` describes the pipeline and the score.

## Pull requests

- Open an issue first for a new source or a change to the score.
- Keep pull requests focused. A source and its tests belong together. Drive-by refactors do not.
- Add tests for behavior someone could break without noticing.
- Do not add a dependency for something the standard library already does.
- Run `gofmt` and `golangci-lint`. CI runs build, vet, lint, and test.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add an Opsgenie incident source
fix: treat Alertmanager's future endsAt as still firing
docs: correct the flap window default
```

## Good first issues

These are self-contained and match the extension points already in the tree:

1. **Opsgenie source.** Implement `source.Source` against the Opsgenie alert API. Map ack and close times the way the PagerDuty source maps acknowledgements. See `docs/extending.md`.
2. **Grafana OnCall source.** Same interface. Alert groups and resolution timestamps are the useful fields.
3. **Slack reporter.** Implement `report.Reporter` and post the "do this week" section, not the whole report, to an incoming webhook.
4. **Prometheus `ALERTS` source.** Range-query `ALERTS` and `ALERTS_FOR_STATE` so teams that never deployed the webhook can still score a week.
5. **Week-over-week diff.** Read a previous report JSON and add a column for the change in noise score per rule.
6. **Shifts that cross midnight.** `business_hours` currently requires `end` after `start`. Follow-the-sun rotations need a window that wraps.
7. **Concrete `for` suggestion.** From the measured gap between resolve and re-fire, recommend a specific `for` duration instead of pointing at the flap window.

If you want to work on one of these, comment on the issue so two people do not start the same one.
