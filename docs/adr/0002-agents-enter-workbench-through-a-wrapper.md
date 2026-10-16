# Worktree agents enter their Workbench through a wrapper, not Orca environment recipes

Orca's `environmentRecipes` can give each worktree its own container over SSH, but it is experimental, appears to work only from the Orca desktop app, and makes the container own its checkout. Orcasm must work from Orca's web UI on hosts without Orca installed, so instead Orca's `orca.yaml` setup/archive scripts start and tear down each Worktree's Stack, and a wrapper installed in the Sandbox as `claude`/`codex`/`opencode` sends the agent into the Workbench when invoked from inside a Worktree (and runs it directly on Main).

## Consequences

- The wrapper must re-plumb what Orca's agent integration expects on the host: the `ORCA_*` hook environment, the hook scripts and agent settings, the hook port, Codex's Orca-managed home, folder trust. This depends on undocumented Orca internals, so the Orca version is pinned and each harness has its own adapter.
- Revisit once recipes are stable and reachable from `orca serve`.
