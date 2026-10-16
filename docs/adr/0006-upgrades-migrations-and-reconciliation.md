# Sandboxes are upgraded in place: ordered migrations, then version reconciliation

Orca releases every few days and orcasm depends on its internals, while recreating a Sandbox would wipe subscription sign-ins and in-flight work. `orcasm upgrade <name>` therefore first runs pending migrations (numbered, idempotent, one-shot structural steps, possibly with host-side parts; the last applied is recorded in the Sandbox label `orcasm.schema`), then reconciles versions declaratively (desired vs installed Orca, Workbench layer). The tested Orca versions come from a compatibility file published from this repo and updated by CI when a new Orca release passes the end-to-end suite, so most Orca bumps need no orcasm release; `--latest` bypasses it. Harnesses float to latest; only Orca and msb are pinned.

## Consequences

- CI must track Orca releases and run the end-to-end suite (driven by the Wizard's hidden answers file) from day one.
- CI commits the compatibility file straight to `main` (`.github/workflows/orca-watch.yml`), so `main` must let GitHub Actions push: workflow permissions set to read and write, and no branch protection or ruleset requiring pull requests or checks on `main` without a bypass for GitHub Actions.
- Upgrades confirm before restarting Orca (it kills agent sessions); Workbenches are recreated lazily on next use, never mid-task.
