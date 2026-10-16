# Provisions a fresh ubuntu-systemd:24.04 Sandbox. Runs as root via
# `msb exec ... bash -c <this> provision <user> <hostport> <orca-version> <main> \
#   <git-name> <git-email> [harness...]` (empty git name: no identity shared).
# Every step is safe to re-run.
set -euo pipefail
user=$1 hostport=$2 orca_version=$3 main=$4 git_name=$5 git_email=$6
shift 6
harnesses=("$@")
export DEBIAN_FRONTEND=noninteractive
step() { echo "==> $*"; }

step "Installing base packages, Docker and gh"
apt-get update -qq
apt-get install -y -qq --no-install-recommends >/dev/null \
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
# Main (the Sandbox's workdir) is created before boot, so the home directory
# already exists and useradd skips /etc/skel.
cp -rT --update=none /etc/skel "/home/$user"
usermod -aG docker "$user"
chown -R "$user:$user" "/home/$user"
mkdir -p "$main"
chown "$user:$user" "$main"
if [ -n "$git_name" ]; then
  runuser -u "$user" -- git config --global user.name "$git_name"
  runuser -u "$user" -- git config --global user.email "$git_email"
fi

step "Starting Docker"
# Docker 29 defaults to the containerd image store under /var/lib/containerd,
# on the overlay root, where overlay mounts fail with EINVAL. The classic
# store keeps everything on the /var/lib/docker disk.
mkdir -p /etc/docker
[ -e /etc/docker/daemon.json ] || echo '{"features":{"containerd-snapshotter":false}}' >/etc/docker/daemon.json
systemctl enable docker >/dev/null 2>&1
systemctl restart docker
docker info >/dev/null

step "Installing Orca $orca_version"
if [ "$(dpkg-query -W -f='${Version}' orca-ide 2>/dev/null || true)" != "$orca_version" ]; then
  deb=/tmp/orca-ide_${orca_version}.deb
  curl -fsSL -o "$deb" "https://github.com/stablyai/orca/releases/download/v${orca_version}/orca-ide_${orca_version}_$(dpkg --print-architecture).deb"
  apt-get install -y -qq --no-install-recommends "$deb" >/dev/null
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
Environment=PATH=/home/$user/.local/bin:/usr/local/bin:/usr/bin:/bin
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
