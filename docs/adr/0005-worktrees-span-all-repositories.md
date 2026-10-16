# Worktrees span every Repository, assembled from Orca-managed worktrees

Orca has no multi-repo worktree (stablyai/orca#21118), but projects split across repos (e.g. `api` + `frontend`) need one Stack running every repo's code for the same task. An orcasm Worktree therefore always has one git worktree per Repository on the same branch name. When a Worktree is created from any Repository in Orca, orcasm's repo-level setup hook (stored in Orca state via `repo.update`, so no committed `orca.yaml`) asks Orca to create the siblings (`orca-ide worktree create --setup skip`) or joins existing ones, and attaches them under one Orca folder workspace. Every sibling is thus Orca-managed and visible with its own diff panel.

## Consequences

- On disk worktrees follow Orca's layout, `/worktrees/<repo>/<worktree>`; only the Workbench presents them grouped, as `/workspace/<repo>`.
- Each sibling is archived individually in Orca; the archive hook counts remaining siblings (under a file lock) and only the last one out tears down the Stack.
- Orca's hook timeout is 2 minutes, so the setup hook only starts the Stack in the background; the agent wrapper waits for the Workbench.
- Cross-repo attachment to one folder workspace is inferred from Orca's source, not yet verified.

## Status: partly implemented; siblings deferred to Milestone 4 (#28)

Milestone 2 copies and registers a single Repository (Main), so the code departs from this decision until [#28](https://github.com/Betree/Orcasm/issues/28) lands. The decision still stands; only its implementation is deferred.

- The setup hook (`internal/worktree`) builds a Stack with one checkout, Main's Repository. It neither creates nor joins siblings, and there is no folder workspace.
- The archive hook tears the Stack down at once. There is no sibling count and no file lock, since nobody else could still need the Stack.
- `stack.Spec` already takes several checkouts (`Checkouts`, resolved per Repository with `/workspace/<Repository>`) and every sibling shares one Compose project name, so #28 only changes the hooks.
