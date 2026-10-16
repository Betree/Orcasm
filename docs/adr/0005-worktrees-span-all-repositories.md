# Worktrees span every Repository, assembled from Orca-managed worktrees

Orca has no multi-repo worktree (stablyai/orca#21118), but projects split across repos (e.g. `api` + `frontend`) need one Stack running every repo's code for the same task. An orcasm Worktree therefore always has one git worktree per Repository on the same branch name. When a Worktree is created from any Repository in Orca, orcasm's repo-level setup hook (stored in Orca state via `repo.update`, so no committed `orca.yaml`) asks Orca to create the siblings (`orca-ide worktree create --setup skip`) or joins existing ones, and attaches them under one Orca folder workspace. Every sibling is thus Orca-managed and visible with its own diff panel.

## Consequences

- On disk worktrees follow Orca's layout, `/worktrees/<repo>/<worktree>`; only the Workbench presents them grouped, as `/workspace/<repo>`.
- Each sibling is archived individually in Orca; the archive hook counts remaining siblings (under a file lock) and only the last one out tears down the Stack.
- Orca's hook timeout is 2 minutes, so the setup hook only starts the Stack in the background; the agent wrapper waits for the Workbench.
- Cross-repo attachment to one folder workspace is inferred from Orca's source, not yet verified.
