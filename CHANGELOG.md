# Changelog

All notable changes to noisefloor are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- Score Alertmanager history for flap, volume, and actionability.
- Ingest the Alertmanager API, a webhook persisted to SQLite, JSON/NDJSON exports, and optional PagerDuty incidents.
- Read Prometheus rule files and recommend tune, mute, or SLO burn-rate.
- Write Markdown, HTML, and JSON reports, and exit non-zero when a noise threshold is exceeded.
- Ship a weekly GitHub Action, a Dockerfile, and goreleaser release binaries.
