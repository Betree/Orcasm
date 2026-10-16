#!/bin/bash
# orcasm agent wrapper: installed as <agent> (claude, codex, ...) ahead of the
# real one on Orca's PATH. In a Worktree it runs the agent in that Worktree's
# Workbench; anywhere else (Main) it runs the real agent here. See
# docs/research/orca.md, "#16 spike", for why each step exists.
set -u
agent=${0##*/}
real=${ORCASM_AGENT_BIN_DIR:-/usr/local/bin}/$agent
worktrees=${ORCASM_WORKTREES:-$HOME/orca/workspaces}

case "$PWD/" in
  "$worktrees"/*) ;;
  *) exec "$real" "$@" ;;
esac

# The Workbench is the running container labelled with the Worktree path
# that holds $PWD (the longest one, should Worktrees ever nest).
container= best=0
while read -r id path; do
  case "$PWD/" in
    "$path"/*) if [ ${#path} -gt $best ]; then container=$id best=${#path}; fi ;;
  esac
done < <(docker ps --filter label=orcasm.worktree --format '{{.ID}} {{.Label "orcasm.worktree"}}')
if [ -z "$container" ]; then
  echo "orcasm: the Workbench of this Worktree is not running" >&2
  exit 1
fi

# The Workbench mounts this copy of Orca's hook scripts over ~/.orca/agent-hooks.
# Orca's hook server listens on the Sandbox's loopback only, so the copy POSTs
# to $ORCASM_HOOK_HOST (the Sandbox's bridge address, relayed below). It also
# drops the agent PID: that PID is from the Workbench's PID namespace, and
# Orca, probing it in the Sandbox's, would mark the agent ended. Refreshed on
# every launch, since Orca rewrites its scripts when it updates.
hooks=$HOME/.cache/orcasm/agent-hooks
mkdir -p "$hooks"
for f in "$HOME"/.orca/agent-hooks/*.sh; do
  [ -e "$f" ] || continue
  tmp=$hooks/.${f##*/}.$$
  sed -e '1a ORCA_HOOK_AGENT_PID=' \
    -e 's#http://127\.0\.0\.1:#http://${ORCASM_HOOK_HOST:-127.0.0.1}:#g' \
    -e 's#--noproxy "127\.0\.0\.1"#--noproxy "*"#g' "$f" >"$tmp" &&
    chmod 755 "$tmp" && mv -f "$tmp" "$hooks/${f##*/}"
done

# Relay the bridge address to Orca's hook port. The endpoint file holds the
# current port; a shell started before an Orca restart has a stale one.
# The relay outlives this agent and serves every Workbench.
port=${ORCA_AGENT_HOOK_PORT:-}
if [ -r "${ORCA_AGENT_HOOK_ENDPOINT:-}" ]; then
  port=$(sed -n 's/^ORCA_AGENT_HOOK_PORT=//p' "$ORCA_AGENT_HOOK_ENDPOINT")
fi
bridge=${ORCASM_BRIDGE_IP:-172.17.0.1}
if [ -n "$port" ] && ! pgrep -f "TCP-LISTEN:$port,bind=$bridge," >/dev/null; then
  setsid socat "TCP-LISTEN:$port,bind=$bridge,reuseaddr,fork" "TCP:127.0.0.1:$port" </dev/null >/dev/null 2>&1 &
fi

# The agent binary, at the same path in the Workbench (read-only mount of
# the Sandbox's global npm modules). Claude Code's is native (glibc); Codex's
# npm entry point is a Node script, so use its vendored static (musl) build.
bin=$(readlink -f "$real")
if [ "$agent" = codex ]; then
  case $(uname -m) in
    x86_64) native=codex-linux-x64/vendor/x86_64-unknown-linux-musl ;;
    aarch64 | arm64) native=codex-linux-arm64/vendor/aarch64-unknown-linux-musl ;;
    *) native= ;;
  esac
  candidate=${bin%/bin/*}/node_modules/@openai/$native/bin/codex
  if [ -n "$native" ] && [ -x "$candidate" ]; then bin=$(readlink -f "$candidate"); fi
fi

# Orca's agent environment, forwarded by name so values stay out of argv.
env_args=()
for k in $(compgen -e); do
  case $k in
    ORCA_* | CODEX_HOME | CLAUDE_CONFIG_DIR | TERM | TERM_PROGRAM | TERM_PROGRAM_VERSION | COLORTERM | LANG | LC_*)
      env_args+=(-e "$k") ;;
  esac
done
tty=()
if [ -t 0 ] && [ -t 1 ]; then tty=(-t); fi

# Not exec: the PTY's foreground process stays this script, named <agent>,
# which Orca recognizes; `docker` would not be.
docker exec -i "${tty[@]}" -u "$(id -u):$(id -g)" -w "$PWD" -e HOME="$HOME" \
  -e ORCASM_HOOK_HOST=host.docker.internal "${env_args[@]}" "$container" "$bin" "$@"
