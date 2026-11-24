#!/usr/bin/env bash
# Open a GitHub issue for the markdown report, or comment if that title is already open.
set -euo pipefail

report="${REPORT:-noisefloor-out/noisefloor.md}"
if [ ! -f "$report" ]; then
  echo "no report at $report; skipping the issue"
  exit 0
fi

export ISSUE_TITLE="${ISSUE_TITLE:-Alert hygiene report $(date -u +%F)}"
body="$(mktemp)"
trap 'rm -f "$body"' EXIT
cp "$report" "$body"
python3 - "$body" <<'PY'
import os
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
text = path.read_text()
limit = 60000
if len(text) > limit:
    path.write_text(text[:limit] + "\n\nReport truncated to fit a GitHub issue.\n")
PY

existing="$(gh issue list --state open --limit 100 --json number,title | python3 -c '
import json, os, sys
title = os.environ["ISSUE_TITLE"]
for item in json.load(sys.stdin):
    if item.get("title") == title:
        print(item["number"])
        break
')"
if [ -n "$existing" ]; then
  gh issue comment "$existing" --body-file "$body"
  echo "commented on #$existing"
else
  gh issue create --title "$ISSUE_TITLE" --body-file "$body"
fi
