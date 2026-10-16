package sandbox

// The root installs into the Sandbox (agent wrappers, `stack`, orcasm
// itself) share one shape: write some files, then maybe run a few commands.

import "strconv"

// guestFile is a file an installer writes in the Sandbox.
type guestFile struct {
	path    string // absolute, in the Sandbox
	content string // written as is
	mode    string // chmod mode, e.g. "0644"
}

// fileWriter writes its files, each atomically (rename), then goes on with
// what follows it. ORCASM_ROOT relocates every path (tests); the scripts
// following it use $root too.
// Args: <file count> [<path> <mode> <content>]... [args of what follows].
const fileWriter = `set -euo pipefail
root=${ORCASM_ROOT:-}
n=$1
shift
for ((i = 0; i < n; i++)); do
  path=$1 mode=$2 content=$3
  shift 3
  install -d "$root$(dirname "$path")"
  printf '%s' "$content" >"$root$path.new"
  chmod "$mode" "$root$path.new"
  mv -f "$root$path.new" "$root$path"
done
`

// installArgv is the root command named name (its $0) writing files, then
// running the bash script then with args as "$@".
func installArgv(name string, files []guestFile, then string, args ...string) []string {
	argv := []string{"bash", "-c", fileWriter + then, name, strconv.Itoa(len(files))}
	for _, f := range files {
		argv = append(argv, f.path, f.mode, f.content)
	}
	return append(argv, args...)
}
