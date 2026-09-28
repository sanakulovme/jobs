#!/usr/bin/env bash
# One-time provisioning of a fresh Ubuntu (22.04 / 24.04) VPS for FaangJobs:
# Go toolchain, a dedicated system user, the app under /opt/faangjobs, a
# hardened systemd service, Caddy (automatic HTTPS + basic auth), a firewall
# and nightly backups. Safe to re-run: every step checks what's already there.
#
# Usage (as root, on the VPS):
#   curl -fsSL https://raw.githubusercontent.com/sanakulovme/jobs/main/deploy/install.sh -o install.sh
#   sudo DOMAIN=crm.example.com bash install.sh
#
# Optional env: REPO_URL, BRANCH (default main), BASIC_USER (default admin),
# BASIC_PASSWORD (default: generated and printed once), ENABLE_UFW (default 1).
set -euo pipefail

DOMAIN="${DOMAIN:?set DOMAIN, e.g. DOMAIN=crm.example.com (its A record must already point at this server)}"
REPO_URL="${REPO_URL:-https://github.com/sanakulovme/jobs.git}"
BRANCH="${BRANCH:-main}"
BASIC_USER="${BASIC_USER:-admin}"
BASIC_PASSWORD="${BASIC_PASSWORD:-}"
ENABLE_UFW="${ENABLE_UFW:-1}"

APP_DIR=/opt/faangjobs
APP_USER=faangjobs
APP_HOME=/var/lib/faangjobs
GO_ROOT=/usr/local/go

log() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
die() { printf '\033[1;31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }
as_app() { sudo -u "$APP_USER" -H "$@"; }
# version_ge A B: true when version A >= version B (e.g. 1.27.0 >= 1.25).
version_ge() { [[ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" == "$2" ]]; }

[[ $EUID -eq 0 ]] || die "run as root (sudo)"
grep -qi ubuntu /etc/os-release || echo "warning: not Ubuntu — continuing, but only Ubuntu is tested"

log "System packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y git curl ca-certificates gnupg ufw openssl \
	debian-keyring debian-archive-keyring apt-transport-https

log "Caddy (reverse proxy + automatic Let's Encrypt)"
if ! command -v caddy >/dev/null; then
	curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' |
		gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
	curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
		>/etc/apt/sources.list.d/caddy-stable.list
	apt-get update -y
	apt-get install -y caddy
fi

log "App user ($APP_USER)"
if ! id "$APP_USER" >/dev/null 2>&1; then
	useradd --system --home-dir "$APP_HOME" --create-home --shell /usr/sbin/nologin "$APP_USER"
fi

log "Source code ($REPO_URL @ $BRANCH -> $APP_DIR)"
if [[ ! -d "$APP_DIR/.git" ]]; then
	install -d -o "$APP_USER" -g "$APP_USER" "$APP_DIR"
	as_app git clone --branch "$BRANCH" "$REPO_URL" "$APP_DIR" ||
		die "git clone failed — if the repo is private, pass REPO_URL=https://<token>@github.com/sanakulovme/jobs.git (or set up a deploy key)"
fi
install -d -o "$APP_USER" -g "$APP_USER" -m 750 "$APP_DIR/data"

log "Go toolchain"
GO_MIN="$(awk '/^go /{print $2; exit}' "$APP_DIR/go.mod")"
GO_HAVE="$("$GO_ROOT/bin/go" env GOVERSION 2>/dev/null || true)"
if [[ -z "$GO_HAVE" ]] || ! version_ge "${GO_HAVE#go}" "$GO_MIN"; then
	# Ubuntu's golang-go package is too old for go.mod (needs >= $GO_MIN),
	# so install the official release tarball instead.
	GO_LATEST="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)"
	ARCH="$(dpkg --print-architecture)" # amd64 / arm64
	curl -fsSL "https://go.dev/dl/${GO_LATEST}.linux-${ARCH}.tar.gz" -o /tmp/go.tgz
	rm -rf "$GO_ROOT"
	tar -C /usr/local -xzf /tmp/go.tgz
	rm -f /tmp/go.tgz
	ln -sf "$GO_ROOT/bin/go" /usr/local/bin/go
fi
"$GO_ROOT/bin/go" version

log "Secrets (.env)"
ENV_FILE="$APP_DIR/.env"
if [[ ! -f "$ENV_FILE" ]]; then
	cp "$APP_DIR/.env.example" "$ENV_FILE"
	sed -i "s|^FAANGJOBS_GOOGLE_REDIRECT_URL=.*|FAANGJOBS_GOOGLE_REDIRECT_URL=https://${DOMAIN}/api/crm/gmail/callback|" "$ENV_FILE"
	echo "created $ENV_FILE — fill in FAANGJOBS_GOOGLE_CLIENT_ID / _SECRET, then: systemctl restart faangjobs"
fi
chown "$APP_USER:$APP_USER" "$ENV_FILE"
chmod 600 "$ENV_FILE"

log "Build + systemd service"
SKIP_PULL=1 bash "$APP_DIR/deploy/update.sh"
systemctl enable faangjobs >/dev/null

log "Caddy config for $DOMAIN"
if [[ -n "$BASIC_PASSWORD" ]] || ! grep -q "^${DOMAIN} {" /etc/caddy/Caddyfile 2>/dev/null; then
	GENERATED=0
	if [[ -z "$BASIC_PASSWORD" ]]; then
		BASIC_PASSWORD="$(openssl rand -base64 18 | tr -d '/+=')"
		GENERATED=1
	fi
	HASH="$(caddy hash-password --plaintext "$BASIC_PASSWORD")"
	sed -e "s|__DOMAIN__|${DOMAIN}|g" \
		-e "s|__BASIC_USER__|${BASIC_USER}|g" \
		-e "s|__BASIC_HASH__|${HASH}|g" \
		"$APP_DIR/deploy/Caddyfile.template" >/etc/caddy/Caddyfile
	caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
	systemctl reload caddy || systemctl restart caddy
	if [[ $GENERATED == 1 ]]; then
		printf '\n\033[1;33mCRM login (shown ONCE — save it now):\n  user: %s\n  pass: %s\033[0m\n' "$BASIC_USER" "$BASIC_PASSWORD"
	fi
else
	echo "Caddyfile already configured for $DOMAIN — left untouched (pass BASIC_PASSWORD=... to reset the login)"
fi

if [[ "$ENABLE_UFW" == 1 ]]; then
	log "Firewall (ufw)"
	# Keep whatever port(s) sshd actually listens on open, so enabling the
	# firewall never locks us out of a server with a non-standard SSH port.
	for p in $(sshd -T 2>/dev/null | awk '$1=="port"{print $2}'); do ufw allow "$p/tcp" >/dev/null; done
	ufw allow OpenSSH >/dev/null 2>&1 || true
	ufw allow 80/tcp >/dev/null
	ufw allow 443/tcp >/dev/null
	ufw allow 443/udp >/dev/null # HTTP/3
	ufw --force enable
	ufw status
fi

log "Nightly backups (03:30, keeps 14 days in /var/backups/faangjobs)"
cat >/etc/cron.d/faangjobs-backup <<EOF
30 3 * * * root /bin/bash $APP_DIR/deploy/backup.sh >>/var/log/faangjobs-backup.log 2>&1
EOF
chmod 644 /etc/cron.d/faangjobs-backup

log "Done"
cat <<EOF
  App:      https://${DOMAIN}/        (CRM: https://${DOMAIN}/crm)
  Health:   curl -s https://${DOMAIN}/healthz
  Logs:     journalctl -u faangjobs -f
  Update:   sudo bash $APP_DIR/deploy/update.sh
  Secrets:  $ENV_FILE  (restart after editing: systemctl restart faangjobs)

  Google Cloud Console -> Credentials -> your OAuth client -> add this
  Authorized redirect URI:  https://${DOMAIN}/api/crm/gmail/callback
EOF
