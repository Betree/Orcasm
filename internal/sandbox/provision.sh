# Provisions a fresh ubuntu-systemd:24.04 Sandbox. Runs as root, after
# orca.sh (which defines install_orca, install_harnesses and start_orca), via
# `msb exec ... bash -c <orca.sh + this> provision <user> <hostport> <orca-version> <main> \
#   <git-name> <git-email> [harness...]` (empty git name: no identity shared).
# Every step is safe to re-run.
set -euo pipefail
user=$1 hostport=$2 orca_version=$3 main=$4 git_name=$5 git_email=$6
shift 6
harnesses=("$@")
export DEBIAN_FRONTEND=noninteractive
step() { echo "==> $*"; }

step "Installing base packages, Docker and gh"
# Right after boot apt-get update sometimes fails with "Unknown error
# executing apt-key"; a retry moments later works.
apt-get update -qq || { sleep 5; apt-get update -qq; }
apt-get install -y -qq --no-install-recommends >/dev/null \
  ca-certificates curl wget tar git rsync less \
  docker.io docker-compose-v2 gh \
  nodejs npm \
  libasound2t64

step "Trusting microsandbox's TLS-interception CA"
# With secrets, msb intercepts TLS with its own CA (docs/research/microsandbox.md §6).
# msb drops it in /usr/local/share/ca-certificates at boot; install it there
# ourselves too (from msb's runtime mount) so update-ca-certificates keeps it
# in the system store whatever the boot order, for git, curl, gh, Docker's
# daemon and anything else using OpenSSL/GnuTLS/Go defaults.
msb_ca=/.msb/tls/ca.pem
if [ -s "$msb_ca" ]; then
  install -m 0644 "$msb_ca" /usr/local/share/ca-certificates/microsandbox-ca.crt
fi
update-ca-certificates >/dev/null
# Tools with their own CA list read these; msb exec sets them, but SSH
# sessions and systemd services (Orca, agents) only see /etc/environment.
# Docker containers keep their own CA bundles: Workbenches must mount
# $ca_bundle (or the CA) themselves.
for var in NODE_EXTRA_CA_CERTS SSL_CERT_FILE CURL_CA_BUNDLE REQUESTS_CA_BUNDLE; do
  grep -q "^$var=" /etc/environment || echo "$var=$ca_bundle" >>/etc/environment
done
if [ -s "$msb_ca" ] && ! curl -fsS -o /dev/null --cacert "$ca_bundle" https://github.com; then
  echo "the system CA store does not trust msb's TLS interception" >&2
  exit 1
fi

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
# The XDG location, which `git config --global` then writes to: Workbenches
# mount ~/.config/git, so agents there commit with the same identity.
runuser -u "$user" -- mkdir -p "/home/$user/.config/git"
runuser -u "$user" -- touch "/home/$user/.config/git/config"
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

install_orca "$orca_version"
install_harnesses "${harnesses[@]}"

# Claude keeps its global config (~/.claude.json, replaced by rename) in
# ~/.claude instead, a directory Workbenches can mount and see updates in.
claude_dir=/home/$user/.claude
grep -q "^CLAUDE_CONFIG_DIR=" /etc/environment || echo "CLAUDE_CONFIG_DIR=$claude_dir" >>/etc/environment

start_orca "$user" "$hostport"
