# Orca and the harnesses, shared by provisioning (provision.sh) and
# `orcasm upgrade` (upgrade.go): this file only defines functions, and is
# prepended to the script calling them. Runs as root. Every function is safe
# to re-run.
ca_bundle=/etc/ssl/certs/ca-certificates.crt
harness_layer=/opt/orcasm/harness

# install_orca <version>: the orca-ide package at that release.
install_orca() {
  local version=$1 deb
  echo "==> Installing Orca $version"
  if [ "$(dpkg-query -W -f='${Version}' orca-ide 2>/dev/null || true)" = "$version" ]; then
    return
  fi
  deb=/tmp/orca-ide_${version}.deb
  curl -fsSL -o "$deb" "https://github.com/stablyai/orca/releases/download/v${version}/orca-ide_${version}_$(dpkg --print-architecture).deb"
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$deb" >/dev/null
  rm -f "$deb"
}

# install_harnesses <harness...>: each harness at its latest release, in the
# harness layer Workbenches mount read-only at the same path;
# /usr/local/bin/<harness> is orcasm's agent wrapper (installed with orcasm
# itself), which runs them on Main or in a Workbench.
install_harnesses() {
  local h pkg installed latest
  for h in "$@"; do
    case $h in
      claude) pkg=@anthropic-ai/claude-code ;;
      codex) pkg=@openai/codex ;;
      opencode) pkg=opencode-ai ;;
      *) echo "unknown harness: $h" >&2; return 1 ;;
    esac
    installed=$(node -p "require('$harness_layer/lib/node_modules/$pkg/package.json').version" 2>/dev/null || true)
    latest=$(NODE_EXTRA_CA_CERTS=$ca_bundle npm view "$pkg" version 2>/dev/null || true)
    if [ -n "$installed" ] && [ "$installed" = "$latest" ]; then
      echo "==> Harness $h ($pkg) $installed is up to date"
      continue
    fi
    echo "==> Installing harness $h ($pkg) ${installed:+$installed → }${latest:-latest}"
    NODE_EXTRA_CA_CERTS=$ca_bundle npm install -g --prefix "$harness_layer" --no-fund --no-audit --loglevel=error "$pkg@latest"
  done
}

# start_orca <user> <hostport>: (re)writes Orca's service and restarts it,
# then waits for its web UI. One serve start = one new pairing device and a
# new Web client URL (docs/research/orca.md), so the unit restarts only on
# failure, rate-limited.
start_orca() {
  local user=$1 hostport=$2 claude_dir=/home/$1/.claude
  echo "==> Starting Orca"
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
Environment=NODE_EXTRA_CA_CERTS=$ca_bundle SSL_CERT_FILE=$ca_bundle CURL_CA_BUNDLE=$ca_bundle REQUESTS_CA_BUNDLE=$ca_bundle
Environment=CLAUDE_CONFIG_DIR=$claude_dir
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
      echo "==> Orca web UI is up"
      return
    fi
    sleep 1
  done
  echo "Orca web UI did not come up within 120s" >&2
  journalctl -u orca --no-pager -n 50 >&2
  return 1
}
