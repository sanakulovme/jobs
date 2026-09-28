#!/usr/bin/env bash
# Pull the latest code, rebuild both binaries, and restart the service.
# Builds to *.new and swaps only on success, so a broken build never takes
# the running server down.
#
# Usage (on the VPS):  sudo bash /opt/faangjobs/deploy/update.sh
# Env: BRANCH (default main), SKIP_PULL=1 to rebuild the current checkout.
set -euo pipefail

APP_DIR=/opt/faangjobs
APP_USER=faangjobs
BRANCH="${BRANCH:-main}"
GO=/usr/local/go/bin/go

[[ $EUID -eq 0 ]] || { echo "run as root (sudo)" >&2; exit 1; }
as_app() { sudo -u "$APP_USER" -H "$@"; }
cd "$APP_DIR"

if [[ "${SKIP_PULL:-0}" != 1 ]]; then
	before="$(as_app git rev-parse --short HEAD)"
	as_app git fetch --quiet origin "$BRANCH"
	as_app git checkout --quiet "$BRANCH"
	as_app git merge --ff-only --quiet "origin/$BRANCH"
	echo "code: $before -> $(as_app git rev-parse --short HEAD)"
fi

echo "building…"
as_app "$GO" build -trimpath -o bin/server.new ./cmd/server
as_app "$GO" build -trimpath -o bin/crawler.new ./cmd/crawler
mv -f bin/server.new bin/server
mv -f bin/crawler.new bin/crawler

# Keep the installed unit in sync with the one in the repo.
install -m 644 deploy/faangjobs.service /etc/systemd/system/faangjobs.service
systemctl daemon-reload
systemctl restart faangjobs

for _ in $(seq 1 30); do
	if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
		echo "faangjobs is up ($(as_app git rev-parse --short HEAD))"
		exit 0
	fi
	sleep 1
done
echo "faangjobs did not become healthy — check: journalctl -u faangjobs -n 50" >&2
exit 1
