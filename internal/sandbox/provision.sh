# Provisions a fresh ubuntu-systemd:24.04 Sandbox. Runs as root via
# `msb exec ... bash -c <this> provision <user> <hostport> <orca-version> [harness...]`.
# Every step is safe to re-run.
set -euo pipefail
user=$1 hostport=$2 orca_version=$3
shift 3
harnesses=("$@")
export DEBIAN_FRONTEND=noninteractive
step() { echo "==> $*"; }

step "Installing base packages, Docker and gh"
apt-get update -q
apt-get install -y -q --no-install-recommends \
  ca-certificates curl wget tar git rsync less \
  docker.io docker-compose-v2 gh \
  nodejs npm \
  libasound2t64

# Node keeps its own CA list; point it at the system store, which holds msb's
# TLS-interception CA when secrets are configured (docs/research/microsandbox.md §6).
ca_bundle=/etc/ssl/certs/ca-certificates.crt
grep -q '^NODE_EXTRA_CA_CERTS=' /etc/environment || echo "NODE_EXTRA_CA_CERTS=$ca_bundle" >>/etc/environment
update-ca-certificates >/dev/null

step "Creating user $user"
if ! id "$user" >/dev/null 2>&1; then
  useradd -m -s /bin/bash "$user"
fi
# The Sandbox's workdir may have pre-created the home directory as root.
cp -rnT /etc/skel "/home/$user"
usermod -aG docker "$user"
chown -R "$user:$user" "/home/$user"
systemctl enable --now docker >/dev/null 2>&1
docker info >/dev/null

step "Installing Orca $orca_version"
if [ "$(dpkg-query -W -f='${Version}' orca-ide 2>/dev/null || true)" != "$orca_version" ]; then
  deb=/tmp/orca-ide_${orca_version}.deb
  curl -fsSL -o "$deb" "https://github.com/stablyai/orca/releases/download/v${orca_version}/orca-ide_${orca_version}_$(dpkg --print-architecture).deb"
  apt-get install -y -q --no-install-recommends "$deb"
  rm -f "$deb"
fi

for h in "${harnesses[@]}"; do
  case $h in
    claude) pkg=@anthropic-ai/claude-code ;;
    codex) pkg=@openai/codex ;;
    opencode) pkg=opencode-ai ;;
    *) echo "unknown harness: $h" >&2; exit 1 ;;
  esac
  step "Installing harness $h ($pkg)"
  NODE_EXTRA_CA_CERTS=$ca_bundle npm install -g --no-fund --no-audit --loglevel=error "$pkg@latest"
done

step "Starting Orca"
# One serve start = one new pairing device and a new Web client URL
# (docs/research/orca.md), so restart only on failure and rate-limit it.
cat >/etc/systemd/system/orca.service <<EOF
[Unit]
Description=Orca runtime server (orcasm)
After=network-online.target docker.service
StartLimitIntervalSec=300
StartLimitBurst=3

[Service]
User=$user
WorkingDirectory=/home/$user
Environment=ORCA_SERVE_RUNTIME=electron
Environment=NODE_EXTRA_CA_CERTS=$ca_bundle
ExecStart=/usr/bin/orca-ide serve --port 6768 --pairing-address 127.0.0.1:$hostport
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable orca >/dev/null 2>&1
systemctl restart orca

for _ in $(seq 120); do
  if curl -fsS -o /dev/null http://127.0.0.1:6768/web-index.html; then
    step "Orca web UI is up"
    exit 0
  fi
  sleep 1
done
echo "Orca web UI did not come up within 120s" >&2
journalctl -u orca --no-pager -n 50 >&2
exit 1
