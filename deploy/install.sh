#!/usr/bin/env bash
#
# First-time installation for tra-commute-bot. Safe to re-run.
#
#   sudo bash deploy/install.sh
#
# Afterwards you still need to:
#   1. fill in the four credentials in /etc/tra-commute/env
#   2. tune /etc/tra-commute/config.yaml to your actual commute
#   3. systemctl enable --now tra-commute.timer

set -euo pipefail

BIN_SRC="${BIN_SRC:-./tracommute}"
BIN_DST=/usr/local/bin/tracommute
CONF_DIR=/etc/tra-commute
UNIT_DIR=/etc/systemd/system
USER_NAME=tracommute
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "This script needs root: sudo bash $0" >&2
  exit 1
fi

echo "==> Creating system account ${USER_NAME}"
# --system with no home and no shell: this account only ever runs a oneshot.
if ! id -u "$USER_NAME" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$USER_NAME"
else
  echo "    already exists, skipping"
fi

echo "==> Installing binary at ${BIN_DST}"
if [[ ! -f "$BIN_SRC" ]]; then
  echo "Binary not found at ${BIN_SRC}" >&2
  echo "Cross-compile it first:" >&2
  echo "  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tracommute ./cmd/tracommute" >&2
  exit 1
fi
install -m 0755 "$BIN_SRC" "$BIN_DST"

echo "==> Creating config directory ${CONF_DIR}"
install -d -m 0755 "$CONF_DIR"

# The config holds no secrets, so 0644 is fine. An existing one is never
# overwritten: it carries parameters that have been tuned against reality.
if [[ -f "$CONF_DIR/config.yaml" ]]; then
  echo "    config.yaml exists, leaving it alone"
else
  install -m 0644 "$HERE/../configs/config.example.yaml" "$CONF_DIR/config.yaml"
  echo "    created config.yaml from the example -- tune it to your commute"
fi

# The credentials file is the only place secrets live: 0600, owned by the
# service account.
if [[ -f "$CONF_DIR/env" ]]; then
  echo "    env exists, leaving it alone"
else
  cat > "$CONF_DIR/env" <<'ENVEOF'
# tra-commute-bot credentials (mode 0600, never committed)
TDX_CLIENT_ID=
TDX_CLIENT_SECRET=
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
ENVEOF
  echo "    created an env template -- fill in the four credentials"
fi
chown "$USER_NAME:$USER_NAME" "$CONF_DIR/env"
chmod 0600 "$CONF_DIR/env"

echo "==> Installing systemd units"
install -m 0644 "$HERE/tra-commute.service" "$UNIT_DIR/"
install -m 0644 "$HERE/tra-commute.timer" "$UNIT_DIR/"
systemctl daemon-reload

cat <<'DONE'

==> Installed. Next steps:

  1. Fill in the credentials
       sudo vi /etc/tra-commute/env

  2. Adjust the schedule, route and clock-in deadline
       sudo vi /etc/tra-commute/config.yaml

  3. Dry run first, to check the message reads correctly.
     Sends nothing and writes no state.
       sudo -u tracommute tracommute -config /etc/tra-commute/config.yaml \
            -env-file /etc/tra-commute/env -dry-run -force

  4. Enable the timer
       sudo systemctl enable --now tra-commute.timer
       systemctl list-timers tra-commute

  5. Watch the logs
       journalctl -u tra-commute -f

DONE
