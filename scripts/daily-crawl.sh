#!/usr/bin/env bash
# Daily MFA vacancy crawl + auto-apply pass, meant to be triggered by
# cron/systemd on the production server. Writes fresh company JSON into
# ./data (the running ./bin/server hot-reloads that directory on its own —
# no restart needed), then matches the refreshed vacancies against every
# candidate.
#
# IMPORTANT: the registry (internal/registry/companies.json) is scoped to
# MFA/Germany only, and the crawler's default -jobs filter keeps developer/IT
# roles. Without -jobs all, this crawl would silently save zero jobs.
#
# SAFETY: the auto-apply step below runs in TEST MODE ONLY (dry run, no
# Gmail sends, no real employer ever contacted) — see cmd/autoapply's own
# safety comment. Do not change -test-mode here without a separate, explicit
# go-ahead from whoever owns this deployment for that specific change; this
# script running unattended on a schedule is exactly the situation that
# two-flag gate exists for.
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

LOG_DIR="$APP_DIR/logs"
mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/crawl-$(date +%Y-%m-%d).log"

{
  echo "===== $(date -Is) starting daily crawl ====="
  ./bin/crawler -data ./data -jobs all
  echo "===== $(date -Is) crawl finished, starting auto-apply pass (test mode) ====="
  ./bin/autoapply -data ./data -test-mode=true
  echo "===== $(date -Is) auto-apply pass finished ====="
} >> "$LOG_FILE" 2>&1

# keep 30 days of logs
find "$LOG_DIR" -name 'crawl-*.log' -mtime +30 -delete
