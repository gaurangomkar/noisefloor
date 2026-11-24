# Alert hygiene report

Window 2026-02-23 00:00 UTC to 2026-03-02 00:00 UTC. Generated 2026-03-02 00:00 UTC.

The noise threshold is exceeded: 1 rule at or above 60.0, and 0 are allowed. `noisefloor report` exits 2.

## Do this week

1. **Tune InstanceDown** (platform, score 68.0). 19 of 20 instances re-fired within 15m of resolving (95.0%). Median time to resolve is 1m. Raise `for` (currently 30s) so the condition must hold through those blips, or set `keep_firing_for` to at least 15m.
2. **Mute PodRestarted** (platform, score 42.2). 100.0% of resolved instances cleared before anyone acknowledged them (measured from acknowledgement timestamps, median time to resolve 1m30s). Stop paging on this rule: route it to a ticket or a dashboard, or delete it if the condition is not actionable.
3. **Tune Edge5xxRate** (edge, score 16.9). 8 of 8 instances overlapped a different fingerprint of this alert (100.0%). Aggregate the expression, or drop high-cardinality labels, so one incident produces one alert.
4. **Tune CertificateExpiring** (security, score 13.8). 6 of 6 pages started outside business hours (100.0%). Page only during the working day, or open a ticket overnight and deliver it at the start of the next business day.
5. **SLO CheckoutLatencyHigh** (payments, score 6.9). 18 instances in the window on a symptom signal (`histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{job="checkout"}[5m]))) > 0.4`). Page on a multi-window burn rate instead, for example 5m and 1h at 14.4x plus 30m and 6h at 6x, and keep a single-threshold rule as a ticket only.

## Summary

| | |
| --- | --- |
| Instances scored | 69 |
| Ignored | 3 |
| Rules | 7 |
| Noisy rules | 1 |
| Worst | InstanceDown (platform) at 68.0 |
| Tune | 3 |
| Mute | 1 |
| SLO burn rate | 1 |
| Keep | 2 |

Sources, before dedupe:

- file:alerts.ndjson: 72

## Rules

| Rule | Team | Score | Action | Fires | Flap | Auto-resolve | Off-hours | Overlap |
| --- | --- | ---: | --- | ---: | ---: | ---: | ---: | ---: |
| InstanceDown | platform | 68.0 | tune | 20 | 95.0% | 100.0% | 0.0% | 0.0% |
| PodRestarted | platform | 42.2 | mute | 12 | 0.0% | 100.0% | 0.0% | 0.0% |
| Edge5xxRate | edge | 16.9 | tune | 8 | 0.0% | 0.0% | 0.0% | 100.0% |
| CertificateExpiring | security | 13.8 | tune | 6 | 0.0% | 0.0% | 100.0% | 0.0% |
| CheckoutLatencyHigh | payments | 6.9 | slo | 18 | 0.0% | 0.0% | 0.0% | 0.0% |
| APIErrorBudgetBurn | payments | 1.1 | keep | 3 | 0.0% | 0.0% | 0.0% | 0.0% |
| APIDown | payments | 0.8 | keep | 2 | 0.0% | 0.0% | 0.0% | 0.0% |

### InstanceDown

| | |
| --- | --- |
| Team | platform |
| Score | 68.0 |
| Recommendation | tune |
| Rule | rules.yaml:31 (platform) |
| For | 30s |

Expression:

```promql
up == 0
```

- 19 of 20 instances re-fired within 15m of resolving (95.0%). Median time to resolve is 1m. Raise `for` (currently 30s) so the condition must hold through those blips, or set `keep_firing_for` to at least 15m.

| Metric | Value |
| --- | --- |
| Fires | 20 |
| Flaps | 19 (95.0%) |
| Median time to resolve | 1m |
| p90 time to resolve | 1m |
| Auto-resolve | 100.0% (grace, 20 samples) |
| Pages | 20 |
| Off-hours pages | 0 (0.0%) |
| Overlapping instances | 0 (0.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| auto_resolve | 2 | 1 | 23.0 |
| flap | 2 | 0.95 | 21.8 |
| short_ttr | 1.5 | 0.9 | 15.5 |
| volume | 1 | 0.6667 | 7.7 |
| duplicate | 1.2 | 0 | 0.0 |
| off_hours | 1 | 0 | 0.0 |

Example labels:

- instance=web-1, severity=page, team=platform

Summary: An instance is down.

### PodRestarted

| | |
| --- | --- |
| Team | platform |
| Score | 42.2 |
| Recommendation | mute |
| Rule | rules.yaml:39 (platform) |
| For | 1m |

Expression:

```promql
increase(kube_pod_container_status_restarts_total[15m]) > 0
```

- 100.0% of resolved instances cleared before anyone acknowledged them (measured from acknowledgement timestamps, median time to resolve 1m30s). Stop paging on this rule: route it to a ticket or a dashboard, or delete it if the condition is not actionable.

| Metric | Value |
| --- | --- |
| Fires | 12 |
| Flaps | 0 (0.0%) |
| Median time to resolve | 1m30s |
| p90 time to resolve | 1m30s |
| Auto-resolve | 100.0% (ack, 12 samples) |
| Pages | 0 |
| Off-hours pages | 0 (0.0%) |
| Overlapping instances | 0 (0.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| auto_resolve | 2 | 1 | 23.0 |
| short_ttr | 1.5 | 0.85 | 14.7 |
| volume | 1 | 0.4 | 4.6 |
| duplicate | 1.2 | 0 | 0.0 |
| flap | 2 | 0 | 0.0 |
| off_hours | 1 | 0 | 0.0 |

Example labels:

- pod=api-0, severity=warning, team=platform
- pod=api-1, severity=warning, team=platform
- pod=api-10, severity=warning, team=platform

Summary: A container restarted.

### Edge5xxRate

| | |
| --- | --- |
| Team | edge |
| Score | 16.9 |
| Recommendation | tune |
| Rule | rules.yaml:51 (edge) |
| For | 5m |

Expression:

```promql
sum by (instance) (rate(http_requests_total{job="edge",status=~"5.."}[5m])) > 0.05
```

- 8 of 8 instances overlapped a different fingerprint of this alert (100.0%). Aggregate the expression, or drop high-cardinality labels, so one incident produces one alert.

| Metric | Value |
| --- | --- |
| Fires | 8 |
| Flaps | 0 (0.0%) |
| Median time to resolve | 30m |
| p90 time to resolve | 30m |
| Auto-resolve | 0.0% (ack, 8 samples) |
| Pages | 8 |
| Off-hours pages | 0 (0.0%) |
| Overlapping instances | 8 (100.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| duplicate | 1.2 | 1 | 13.8 |
| volume | 1 | 0.2667 | 3.1 |
| auto_resolve | 2 | 0 | 0.0 |
| flap | 2 | 0 | 0.0 |
| off_hours | 1 | 0 | 0.0 |
| short_ttr | 1.5 | 0 | 0.0 |

Example labels:

- instance=edge-0, severity=page, team=edge
- instance=edge-1, severity=page, team=edge
- instance=edge-2, severity=page, team=edge

Summary: One edge instance is returning 5xx responses.

### CertificateExpiring

| | |
| --- | --- |
| Team | security |
| Score | 13.8 |
| Recommendation | tune |
| Rule | rules.yaml:61 (security) |
| For | 1h |

Expression:

```promql
(probe_ssl_earliest_cert_expiry - time()) < 14 * 24 * 3600
```

- 6 of 6 pages started outside business hours (100.0%). Page only during the working day, or open a ticket overnight and deliver it at the start of the next business day.

| Metric | Value |
| --- | --- |
| Fires | 6 |
| Flaps | 0 (0.0%) |
| Median time to resolve | 30m |
| p90 time to resolve | 30m |
| Auto-resolve | 0.0% (ack, 6 samples) |
| Pages | 6 |
| Off-hours pages | 6 (100.0%) |
| Overlapping instances | 0 (0.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| off_hours | 1 | 1 | 11.5 |
| volume | 1 | 0.2 | 2.3 |
| auto_resolve | 2 | 0 | 0.0 |
| duplicate | 1.2 | 0 | 0.0 |
| flap | 2 | 0 | 0.0 |
| short_ttr | 1.5 | 0 | 0.0 |

Example labels:

- instance=edge, severity=page, team=security

Summary: A certificate expires within 14 days.

### CheckoutLatencyHigh

| | |
| --- | --- |
| Team | payments |
| Score | 6.9 |
| Recommendation | slo |
| Rule | rules.yaml:4 (checkout) |
| For | 2m |

Expression:

```promql
histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{job="checkout"}[5m]))) > 0.4
```

- 18 instances in the window on a symptom signal (`histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{job="checkout"}[5m]))) > 0.4`). Page on a multi-window burn rate instead, for example 5m and 1h at 14.4x plus 30m and 6h at 6x, and keep a single-threshold rule as a ticket only.

| Metric | Value |
| --- | --- |
| Fires | 18 |
| Flaps | 0 (0.0%) |
| Median time to resolve | 25m |
| p90 time to resolve | 25m |
| Auto-resolve | 0.0% (ack, 18 samples) |
| Pages | 18 |
| Off-hours pages | 0 (0.0%) |
| Overlapping instances | 0 (0.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| volume | 1 | 0.6 | 6.9 |
| auto_resolve | 2 | 0 | 0.0 |
| duplicate | 1.2 | 0 | 0.0 |
| flap | 2 | 0 | 0.0 |
| off_hours | 1 | 0 | 0.0 |
| short_ttr | 1.5 | 0 | 0.0 |

Example labels:

- job=checkout, severity=page, team=payments

Summary: Checkout p99 latency is above 400ms.

Runbook: https://example.com/runbooks/checkout-latency

### APIErrorBudgetBurn

| | |
| --- | --- |
| Team | payments |
| Score | 1.1 |
| Recommendation | keep |
| Rule | rules.yaml:21 (checkout) |
| For | 2m |

Expression:

```promql
job:slo_burnrate1h{job="api"} > 14.4 and job:slo_burnrate5m{job="api"} > 14.4
```

- Noise score 1.1 is under the 60.0 review threshold.

| Metric | Value |
| --- | --- |
| Fires | 3 |
| Flaps | 0 (0.0%) |
| Median time to resolve | 20m |
| p90 time to resolve | 20m |
| Auto-resolve | 0.0% (ack, 3 samples) |
| Pages | 3 |
| Off-hours pages | 0 (0.0%) |
| Overlapping instances | 0 (0.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| volume | 1 | 0.1 | 1.1 |
| auto_resolve | 2 | 0 | 0.0 |
| duplicate | 1.2 | 0 | 0.0 |
| flap | 2 | 0 | 0.0 |
| off_hours | 1 | 0 | 0.0 |
| short_ttr | 1.5 | 0 | 0.0 |

Example labels:

- job=api, severity=page, team=payments

Summary: The API error budget is burning.

### APIDown

| | |
| --- | --- |
| Team | payments |
| Score | 0.8 |
| Recommendation | keep |
| Rule | rules.yaml:13 (checkout) |
| For | 1m |

Expression:

```promql
probe_success{job="checkout"} == 0
```

- Noise score 0.8 is under the 60.0 review threshold.

| Metric | Value |
| --- | --- |
| Fires | 2 |
| Flaps | 0 (0.0%) |
| Median time to resolve | 42m30s |
| p90 time to resolve | 44m30s |
| Auto-resolve | 0.0% (ack, 2 samples) |
| Pages | 2 |
| Off-hours pages | 0 (0.0%) |
| Overlapping instances | 0 (0.0%) |
| Still firing | 0 |
| Started before the window | 0 |
| Silenced | 0 |
| Inhibited | 0 |

| Component | Weight | Value | Points |
| --- | ---: | ---: | ---: |
| volume | 1 | 0.0667 | 0.8 |
| auto_resolve | 2 | 0 | 0.0 |
| duplicate | 1.2 | 0 | 0.0 |
| flap | 2 | 0 | 0.0 |
| off_hours | 1 | 0 | 0.0 |
| short_ttr | 1.5 | 0 | 0.0 |

Example labels:

- job=checkout, severity=page, team=payments

Summary: The checkout probe is failing.

## By team

### edge

- Edge5xxRate: tune, score 16.9, 8 fires

### payments

- CheckoutLatencyHigh: slo, score 6.9, 18 fires
- APIErrorBudgetBurn: keep, score 1.1, 3 fires
- APIDown: keep, score 0.8, 2 fires

### platform

- InstanceDown: tune, score 68.0, 20 fires
- PodRestarted: mute, score 42.2, 12 fires

### security

- CertificateExpiring: tune, score 13.8, 6 fires

## How to read this

- **tune** means the rule is pointing at something real, but it is too sensitive, too broad, or paging at the wrong time.
- **mute** means most instances clear before a person can act. Stop paging. A ticket or a dashboard is enough, or delete the rule.
- **slo** means the signal is a user-facing symptom. Page on a multi-window burn rate and keep the single threshold as a ticket.
- **keep** means this window does not justify a change.

- Loaded 7 alerting rule(s) from 1 file(s).
- Scored 69 instances after ignoring 3 and dropping 0 with no start time.
- A flap is a firing that starts within 15m after the previous instance of the same fingerprint resolved.
- Business hours: Mon, Tue, Wed, Thu, Fri 09:00-17:00 America/New_York.
- A page is an alert whose severity is one of: page, critical, high. Alerts with no severity label count as pages.
- Auto-resolve uses acknowledgement timestamps when a source recorded them. Otherwise a resolution within 5m counts as auto-resolve.
- The noise score is 100 times the weighted mean of volume (fires / 30, capped at 1), flap rate, short time-to-resolve, auto-resolve ratio, off-hours page ratio, and overlap ratio.

