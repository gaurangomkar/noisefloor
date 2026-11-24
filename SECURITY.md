# Security policy

## Reporting a vulnerability

Report vulnerabilities through GitHub private vulnerability reporting:

<https://github.com/gaurangomkar/noisefloor/security/advisories/new>

Do not open a public issue for a security problem. Include the version
(`noisefloor version`), the component (CLI, webhook, SQLite store, a source),
and a way to reproduce the issue. You should hear back within a week.

## Supported versions

Security fixes go into the latest release only. This project is early; pin a
release in CI rather than tracking `main`.

## Operational notes

The webhook listener authenticates callers only when `webhook.token` is set or
`NOISEFLOOR_WEBHOOK_TOKEN` (or the configured `webhook.token_env`) is present
in the environment. With neither set, anyone who can reach the port can write
alert history. Do not expose the listener on an untrusted network without a
token or a network policy that limits who can connect.

The SQLite file stores alert labels, annotations, and acknowledgement times.
Treat it as production data: it often contains hostnames, customer identifiers,
and fragments of queries.

PagerDuty tokens, Alertmanager bearer tokens, and basic-auth passwords should
come from the environment (`token_env`, `bearer_token_env`, `password_env`) or
from a file that is not committed. The tool does not print those values, but
the config file will if you put them there.

Report output is meant to be shared with the team that owns the alerts. Read
it before posting it to a public issue tracker.
