# Orcasm is a single static Go binary

Ease of install and minimal dependencies are primary goals. A static binary needs no runtime on the host (only `msb`, which orcasm installs and pins), installs via `curl | sh` or Homebrew, and Charm's `huh` gives the Wizard multi-select, defaults and validation that bash (3.2 on macOS) cannot offer without extra dependencies. Rust was considered for microsandbox's SDK, but orcasm drives `msb` through its CLI because the SDK is beta with frequent breaking changes. Helpers that run inside the Sandbox may still be small shell scripts.
