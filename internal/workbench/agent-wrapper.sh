#!/bin/bash
# orcasm agent wrapper: installed in the Sandbox as /usr/local/bin/<agent>
# (claude, codex, opencode), the real agents living in the harness layer
# (/opt/orcasm/harness). In a Worktree it runs the agent in that Worktree's
# Workbench, once its Stack is up; anywhere else (Main) it runs the real
# agent here. See docs/research/orca.md, "#16 spike" and "#19", for why each
# step exists.
set -u
agent=${0##*/}
real=${ORCASM_AGENT_BIN_DIR:-/opt/orcasm/harness/bin}/$agent
worktrees=${ORCASM_WORKTREES:-/worktrees} # Orca's <worktrees>/<repo>/<worktree>

case "$PWD/" in
  "$worktrees"/*/*/*) ;;
  *) exec "$real" "$@" ;;
esac

# Waits for the Worktree's Stack, showing its build progress on stderr,
# then names its running Workbench container.
container=$(orcasm worktree workbench "$PWD") || exit 1

# The Workbench mounts this copy of Orca's hook scripts over ~/.orca/agent-hooks
# (stack.HookScriptsCopy). Orca's hook server listens on the Sandbox's
# loopback only, so the copy POSTs to $ORCASM_HOOK_HOST, Docker's bridge,
# where the orcasm-hook-relay unit forwards to Orca. It also drops the agent
# PID: that PID is from the Workbench's PID namespace, and Orca, probing it
# in the Sandbox's, would mark the agent ended. Refreshed on every launch,
# since Orca rewrites its scripts when it starts.
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

extra=()
if [ "$agent" = opencode ]; then
  # OpenCode reports through a plugin Orca writes into its config dir (or a
  # per-launch overlay in $OPENCODE_CONFIG_DIR), posting to the loopback.
  # The Workbench gets a copy of that dir posting to the bridge instead
  # (stack.OpenCodeConfigCopy), one per running wrapper.
  copies=$HOME/.cache/orcasm/opencode-config
  mkdir -p "$copies"
  for d in "$copies"/*; do
    [ -d "$d" ] || continue
    kill -0 "${d##*/}" 2>/dev/null || rm -rf "$d"
  done
  copy=$copies/$$
  mkdir -p "$copy"
  src=${OPENCODE_CONFIG_DIR:-$HOME/.config/opencode}
  if [ -d "$src" ]; then cp -a "$src/." "$copy/"; fi
  find "$copy" -type f \( -name '*.js' -o -name '*.mjs' -o -name '*.ts' \) -exec \
    sed -i 's#http://127\.0\.0\.1:#http://${process.env.ORCASM_HOOK_HOST || "127.0.0.1"}:#g' {} +
  extra+=(-e "OPENCODE_CONFIG_DIR=$copy")
fi

# The agent binary, at the same path in the Workbench (the harness layer is
# mounted read-only). Claude Code's and OpenCode's are native (glibc);
# Codex's npm entry point is a Node script, so use its vendored static
# (musl) build.
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
  -e ORCASM_HOOK_HOST=host.docker.internal "${env_args[@]}" "${extra[@]}" "$container" "$bin" "$@"
