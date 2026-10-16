#!/bin/bash
# stack: runs Main's Stack, on demand only (installed by orcasm; never started
# automatically). Plain `docker compose` over the Project's Compose files,
# published ports unchanged, as Compose project "main".
set -euo pipefail
conf=${ORCASM_CONF_DIR:-/etc/orcasm}

usage() {
  cat >&2 <<'USAGE'
usage: stack up [args]    start Main's Stack in the background (docker compose up -d)
       stack down [args]  stop and remove Main's Stack (docker compose down)
       stack <cmd> [args] any other docker compose command (ps, logs, exec, ...)
USAGE
}

case ${1:-} in
  "" | -h | --help | help) usage; exit 2 ;;
esac

main=$(<"$conf/main")
files=()
while IFS= read -r f; do
  if [ -n "$f" ]; then files+=(-f "$main/$f"); fi
done <"$conf/main-compose-files"
if [ ${#files[@]} -eq 0 ]; then
  echo "stack: this Project has no Compose files (compose_files in orcasm.toml)" >&2
  exit 1
fi

# Compose resolves relative paths from the first file's directory, as when
# run by hand from Main.
compose=(docker compose -p main "${files[@]}")
cmd=$1
shift
case $cmd in
  up)
    # The Worktree proxy (orcasm-proxy) owns the Sandbox's port 80 for
    # http://<worktree>.localhost. A config error is left for `up` to report.
    config=$("${compose[@]}" config 2>/dev/null || true)
    if grep -Eq '^ +published: "?80"?$' <<<"$config"; then
      echo "stack: Main's Stack publishes port 80, which the Worktree proxy uses to serve" >&2
      echo "http://<worktree>.localhost in the Sandbox. Publish it on another host port" >&2
      echo "(e.g. \"8080:80\") in the Compose files." >&2
      exit 1
    fi
    exec "${compose[@]}" up -d "$@" ;;
  *) exec "${compose[@]}" "$cmd" "$@" ;;
esac
