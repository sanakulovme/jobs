#!/usr/bin/env bash
# Snapshot the whole data dir (CRM records, uploaded CVs, Gmail tokens,
# on-demand scrape pools) into a dated tarball; keep the last KEEP_DAYS days.
# Installed as a nightly cron job by install.sh. The archives contain
# candidate PII and OAuth tokens, so they are root-only (umask 077).
#
# Restore:  systemctl stop faangjobs
#           tar -xzf /var/backups/faangjobs/<file>.tar.gz -C /opt/faangjobs
#           chown -R faangjobs:faangjobs /opt/faangjobs/data
#           systemctl start faangjobs
set -euo pipefail
umask 077

APP_DIR=/opt/faangjobs
BACKUP_DIR="${BACKUP_DIR:-/var/backups/faangjobs}"
KEEP_DAYS="${KEEP_DAYS:-14}"

mkdir -p "$BACKUP_DIR"
out="$BACKUP_DIR/faangjobs-$(date +%Y%m%d-%H%M%S).tar.gz"
tar -czf "$out" -C "$APP_DIR" data
find "$BACKUP_DIR" -name 'faangjobs-*.tar.gz' -mtime +"$KEEP_DAYS" -delete
echo "$(date -Is) backup ok: $out ($(du -h "$out" | cut -f1))"
