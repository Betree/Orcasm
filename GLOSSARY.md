# Orcasm

Orcasm packages microsandbox and Orca into one wizard that gives a project an isolated VM where many coding agents work in parallel.

## Language

**Project**:
A code repository on the host that a user hands to orcasm. A Project can have several Sandboxes.
_Avoid_: Repo, app

**Repository**:
One git repository inside a Project; a Project holds one or several, and each picked Repository becomes an Orca project.
_Avoid_: Monorepo, submodule, sub-repo

**Sandbox**:
A named microsandbox VM holding a copy of one Project, running Orca and Docker.
_Avoid_: VM, box, machine

**Wizard**:
The interactive flow through which every orcasm input is given when creating a Sandbox.
_Avoid_: Setup script, installer

**Main**:
The Project's primary checkout inside the Sandbox, where agents run directly in the Sandbox without a container.
_Avoid_: Root, base worktree

**Worktree**:
A named unit of work spanning every Repository of the Project, with one git worktree per Repository on the same branch name, and its own Workbench and Stack.
_Avoid_: Feature, branch, workspace

**Workbench**:
The container belonging to one Worktree, where that Worktree's agents and their commands run. Either a service from the Project's Compose files or a generic fallback.
_Avoid_: Dev container, agent container

**Migration**:
A numbered, one-shot structural change that `orcasm upgrade` applies in place to a Sandbox made by an older orcasm; the last one applied is recorded on the Sandbox.
_Avoid_: Schema change, patch

**Stack**:
The services from one or more Compose files, run together as a single isolated unit for one Worktree (or for Main, on demand).
_Avoid_: Environment, services
