# Orca (stablyai/orca): research for orcasm

Researched 2026-10-16. Sources: the repo `github.com/stablyai/orca` at `main` commit
[`3314659`](https://github.com/stablyai/orca/tree/3314659c10c4bfd929cf2c453427f67af535b0e9)
(the docs site sources live in `docs/site/content/docs/`), the release tag `v1.4.227`, and the locally
installed AppImage (v1.4.224, mounted at `/tmp/.mount_orca.*`, CLI `resources/bin/orca-ide --help`).

`R` below means `https://github.com/stablyai/orca/blob/3314659c10c4bfd929cf2c453427f67af535b0e9`.

## 0. Identity: which "orca" is which

- The product is **stablyai/orca**: "the ADE for working with a fleet of parallel agents". The homepage
  is **onOrca.dev**, not orca.dev (repo metadata, `gh repo view stablyai/orca`).
- **`/usr/bin/orca` on this machine is not that product.** It is the GNOME Orca screen reader
  (`rpm -qf` gives `orca-50.3-1.fc44`, a Python script). The agent IDE is installed as
  `~/AppImages/orca.appimage` (desktop file `~/.local/share/applications/orca.desktop`).
- On Linux the Orca CLI is named **`orca-ide`** to avoid that clash. Inside Orca-managed terminals a
  shim makes bare `orca` work too ([install.mdx](R/docs/site/content/docs/install.mdx), "The CLI command is
  `orca-ide`"). orcasm scripts should always call `orca-ide`.

## 1. Version, license, install, headless

- **Latest stable: v1.4.227**, published 2026-10-16 (`gh release list`). Releases are frequent: 1.4.222,
  1.4.223, 1.4.224 and 1.4.227 came out within ten days. The installed AppImage is 1.4.224.
- **License: MIT** (GitHub `licenseInfo`, [LICENSE](R/LICENSE)).
- **It is an Electron app.** The AppImage contains Electron/Chromium (`chrome-sandbox`, `app.asar`,
  `LICENSE.electron.txt`).
- **Linux packages**: the AppImage (x64 and arm64, self-updating), `orca-ide_<v>_amd64.deb` / `arm64.deb`,
  and `orca-ide-<v>.x86_64.rpm` / `aarch64.rpm` (the .deb and .rpm do not self-update). Stable AppImage
  URL: `https://github.com/stablyai/orca/releases/latest/download/orca-linux.AppImage`. The .deb and .rpm
  put `/usr/bin/orca-ide` on PATH; with the AppImage you register `~/.local/bin/orca-ide` from
  Settings ([install.mdx](R/docs/site/content/docs/install.mdx)). There is no apt or yum repository
  yet (issue #18086).
- **Headless mode exists: `orca-ide serve`** "Start an Orca runtime server without opening a desktop
  window" ([remote-servers.mdx](R/docs/site/content/docs/remote-servers.mdx),
  [src/cli/specs/serve.ts](R/src/cli/specs/serve.ts)). It has two backends:
  - **orcad**, the default: a plain Node server without Electron, shipped as `resources/orcad-template`.
    Orca falls back to Electron when orcad can't prepare or preflight, or when the profile has SSH
    targets ([orcad-local-serve-selection.ts](R/src/main/orcad/orcad-local-serve-selection.ts)).
  - **Electron**: force it with `ORCA_SERVE_RUNTIME=electron`. On Linux this needs an X display. If none
    is present Orca **starts Xvfb itself**, so the VM must have the `xvfb` / `xorg-x11-server-Xvfb`
    package. Orca also forces `--ozone-platform=x11`, `disable-gpu` and `disable-dev-shm-usage`
    ([ensure-virtual-display.ts](R/src/main/startup/ensure-virtual-display.ts),
    [headless-serve-ozone-platform.ts](R/src/shared/headless-serve-ozone-platform.ts)).
- On `main` only (not in v1.4.227): `orca serve service install|status|uninstall` writes a systemd user
  unit that runs `orca serve --supervise --no-pairing`, plus a `--supervise` flag for service managers
  ([serve.ts](R/src/cli/specs/serve.ts)). The v1.4.227 tag's `serve.ts` has no `service` subcommand.
- Optional caps per terminal on Linux: `ORCA_TERMINAL_MEMORY_MAX` and `ORCA_TERMINAL_MEMORY_SWAP_MAX`.
  They need systemd with cgroup v2 and `loginctl enable-linger`
  ([remote-servers.mdx](R/docs/site/content/docs/remote-servers.mdx)).

## 2. Web interface

- **A browser web client exists.** Orca builds `out/web/web-index.html` from the renderer
  ([vite.web.config.ts](R/vite.web.config.ts), `base: './'` so it works behind a reverse-proxy path
  prefix). The installed AppImage's `app.asar` contains `/out/web/web-index.html`.
- The same WebSocket listener that serves the runtime RPC also serves the bundle as static files
  (`staticRoot: this.webClientRoot` in
  [runtime-rpc-lifecycle.ts](R/src/main/runtime/runtime-rpc/runtime-rpc-lifecycle.ts),
  [static-web-client-handler.ts](R/src/main/runtime/rpc/static-web-client-handler.ts)). When the bundle
  is present, `serve` prints a `Web client URL:` line of the form
  `http(s)://<pairing-address>:<port>/web-index.html#pairing=<url-encoded orca://pair?... code>`.
  `https` is used when the pairing address is `wss://`. The credential sits in the URL fragment so it
  stays out of proxy logs ([runtime-rpc-pairing-types.ts `createWebClientUrl`](R/src/main/runtime/runtime-rpc/runtime-rpc-pairing-types.ts)).
- **Important caveat (read from source, not tested): only the Electron serve backend wires up the web
  client.** `main-process-runtime-launch.ts` passes `webClientRoot: getBundledWebClientRoot()`. The
  orcad entry builds `OrcaRuntimeRpcServer` without `webClientRoot`, in both `main` and the v1.4.227 tag
  ([orcad-entry.ts](R/src/main/orcad/orcad-entry.ts)). orcasm should therefore probably run
  `ORCA_SERVE_RUNTIME=electron orca-ide serve ...` with Xvfb in the VM. Verify this empirically.
- **Port**: **6768 is the default.** Without `--port`, serve tries 6768 and moves to another port when
  6768 is taken. With `--port N`, serve fails if N is busy ([serve.ts](R/src/cli/specs/serve.ts) notes).
  `pnpm dev` uses 6769. orcasm should pass `--port 6768` explicitly for a deterministic forward.
- **Bind address**: `orca serve` binds **`0.0.0.0`**. The CLI hard-codes `--bind 0.0.0.0` for orcad
  ([serve-orcad-launch.ts `orcadServeArgs`](R/src/cli/runtime/serve-orcad-launch.ts)), and Electron
  serve sets `exposeNetworkByDefault`, which gives `WS_BIND_HOST_ALL_INTERFACES = '0.0.0.0'`
  ([runtime-rpc-pairing-types.ts](R/src/main/runtime/runtime-rpc/runtime-rpc-pairing-types.ts)).
  `orca serve` has no `--host` or `--bind` flag; the per-workspace-env skill guide says so explicitly.
  The raw `orcad` binary does take `--bind <ip>` (default 127.0.0.1)
  ([orcad-command-arguments.ts](R/src/main/orcad/orcad-command-arguments.ts)). The desktop app stays on
  loopback until a device pairs.
- **`--pairing-address`** changes only the address advertised inside the pairing link and web URL. It
  accepts host, host:port, IP, or a `ws(s)://` URL; HTTP(S) URLs are normalized to WS. For orcasm, set it
  to the host-reachable address of the forwarded port (for example `127.0.0.1:6768` if the host
  forwards that port). The docs warn "Do not select 127.0.0.1 for another computer", but here the
  browser runs on the host where the forward lives, so loopback is correct.
- **Auth**: pairing-based. The server creates a pairing offer; each paired client gets a **separate,
  revocable token**, listed under Settings → Remote Orca Servers → Shared Server Access. The pairing
  URL "grants access to this Orca runtime. Treat it like a password". There is an E2EE keypair
  (`orca-e2ee-keypair.json`) and a device registry (`orca-devices.json`) in userData. `--no-pairing`
  skips creating an offer. The docs say not to expose the port to the public internet and to prefer
  Tailscale, WireGuard, SSH forwarding, or an authenticated tunnel
  ([remote-servers.mdx](R/docs/site/content/docs/remote-servers.mdx)). The server has no username or
  password login.
- Features the web client lacks: Download (it needs Electron's save dialog), Keep-awake
  ([ssh.mdx](R/docs/site/content/docs/ssh.mdx), [settings.mdx](R/docs/site/content/docs/settings.mdx)).

## 3. Integrated browser in web mode

- Each worktree has its own Chromium browser
  ([browser/overview.mdx](R/docs/site/content/docs/browser/overview.mdx)). For remote workspaces there is
  a setting, Settings → Browser → Remote server workspaces: **This device** (rendered locally while
  traffic is proxied through the host) or **Server (streamed)**.
- In the **web client**, local rendering is disabled. `installBrowserClientPageRenderer` returns null
  when `window.__ORCA_WEB_CLIENT__` is set
  ([browser-client-page-renderer-installation.ts](R/src/renderer/src/components/browser-pane/browser-client-page-renderer-installation.ts)).
  The pages therefore run **server-side** and reach the browser as a **CDP screencast stream**, with
  input forwarded back (`components/browser-pane/stream-remote/*`,
  `shared/browser-screencast-protocol`). This is inferred from source; the docs don't state it for the
  web client.
- Where the server-side browser runs:
  - Electron serve: hidden offscreen BrowserWindows on the Xvfb display.
  - orcad: an "external Chromium" process (`src/main/orcad/external-chromium-*.ts`). The AppImage also
    bundles `resources/agent-browser-linux-x64`.
- Implication for orcasm: dev servers inside the VM or compose (for example `localhost:3000`) are
  reachable from the streamed browser without extra port forwarding. Agents can drive the same browser
  through `orca snapshot/click/goto ...`.

## 4. Projects and repos

- Docs path: on first launch you add a repo from the UI
  ([install.mdx](R/docs/site/content/docs/install.mdx)).
- **CLI path, which needs a running runtime** ("Most commands require a running Orca runtime"):
  - `orca-ide repo add --path <path> [--json]`, `repo list`, `repo set-base-ref`
  - `project setup-existing-folder`, `project setup-clone`
  - `worktree create ...`

  All of these talk to the live runtime over RPC. For a remote runtime, pass `--pairing-code`,
  `--environment`, `ORCA_PAIRING_CODE` or `ORCA_ENVIRONMENT`. This is the supported non-interactive way
  to pre-register a project: start `serve`, then run `repo add`.
- **On-disk state** (Linux): userData is `${ORCA_USER_DATA_PATH:-${XDG_CONFIG_HOME:-~/.config}/orca}`
  (skill guide, section 3). orcad also honors `ORCA_USER_DATA`. Profile data sits in
  `profiles/local-default/` (`profile-state.db` in SQLite, `orca-data.json`, ...). Other files:
  `orca-runtime.json`, `orca-devices.json`, `orca-e2ee-keypair.json`, `agent-hooks/endpoint.env`,
  `orchestration.db`. Keybindings live in `~/.orca/keybindings.json`.
- **Per-repo config** checked into the repo: `orca.yaml` and `.worktreeinclude` (see section 5), plus
  `.orca/issue-command`.
- No documented declarative global settings file or import mechanism exists. Settings live in SQLite,
  so seed state through the CLI rather than by editing files.
- **Do not bake userData into a VM image.** The pairing keypair, device tokens and
  `agent-session-authority.key` would be shared by every VM: "Two VMs from one such snapshot emitted
  identical deviceToken" ([skill-guides/orca-per-workspace-env.md](R/skill-guides/orca-per-workspace-env.md),
  section 3).

## 5. Worktrees

- Orca runs `git worktree` on the target host (local, SSH or remote server). The default root is
  **`~/orca/workspaces`** with `nestWorkspaces: true`, so a worktree lands at
  `~/orca/workspaces/<repo>/<name>` ([src/shared/constants.ts `getDefaultWorkspaceDir`](R/src/shared/constants.ts),
  [default-global-settings.ts](R/src/shared/default-global-settings.ts); the CLI example path is
  `/Users/me/orca/workspaces/orca/cli-test-1`). Change it in Settings (`workspaceDir`).
- CLI: `orca-ide worktree create --name X [--repo id:|name:|path:] [--agent claude] [--prompt ...]
  [--setup run|skip|inherit] [--base-branch ref] [--activate] --json`, plus `worktree rm` and
  `worktree list`.
- **`orca.yaml`** at the repo root ([model/orca-yaml.mdx](R/docs/site/content/docs/model/orca-yaml.mdx)):
  - `scripts.setup`: runs in a "Setup" terminal of the new worktree. On Linux it is a Bash runner with
    `set -e` and gets the env vars `ORCA_ROOT_PATH`, `ORCA_WORKTREE_PATH` and `ORCA_WORKSPACE_NAME`.
  - `scripts.archive`: runs before removal. From the CLI it needs `--run-hooks`.
  - `setupAgentStartupPolicy: start-immediately | wait-for-setup`
  - `defaultTabs`, a list of `{title, command, color}`
  - `worktree.sharedDirectories`: gitignored directories symlinked from the primary checkout
  - `issueCommand`
  - `environmentRecipes`, see section 6
- **`.worktreeinclude`** lists gitignored files to copy into each new worktree (for example `.env`).
  It takes literal paths only, with a budget of 2 GiB and 50k entries.
- Which checkout supplies what:
  - From the new worktree's `orca.yaml`: setup and `defaultTabs`.
  - From the primary checkout: shared directories, `.worktreeinclude`, archive and `environmentRecipes`.
- Setup policy is set in Settings → Repository (run, ask or skip; new repos run setup by default). The
  command source is chosen per repo: `shared-only`, `local-only` or `run-both`. **"A valid file does not
  bypass Orca's command approval"**: shared YAML commands go through an approval/trust flow that can
  reuse saved trust.
- **Natural compose hook for orcasm:** in `scripts.setup`, run
  `docker compose -p "$ORCA_WORKSPACE_NAME" up -d`; in `scripts.archive`, run
  `docker compose -p ... down -v`. Each worktree then gets its own compose project.

## 6. "Cloud VM" / per-workspace environments

([ways-to-run.mdx](R/docs/site/content/docs/ways-to-run.mdx) section 4,
[skill-guides/orca-per-workspace-env.md](R/skill-guides/orca-per-workspace-env.md) and `references/*.md`)

- **What it is**: an experimental feature (Settings → Experimental → **Cloud VM**). Each worktree can
  boot its own disposable environment from a **recipe** in `orca.yaml`. Orca is "a thin wrapper": it
  runs **your** lifecycle scripts, and the provider and billing belong to the user (Vercel Sandbox,
  Fly, Modal, an SSH host, local Docker...).
- **Config**:
  ```yaml
  environmentRecipes:
    - id: cloud-sandbox
      name: Cloud Sandbox
      create: ./scripts/orca-vm/x-create.sh     # required; prints ONE JSON object on stdout
      suspend: ./scripts/orca-vm/x-suspend.sh   # optional; lifecycle JSON on stdin
      resume:  ./scripts/orca-vm/x-resume.sh    # must re-emit fresh recipe JSON
      destroy: ./scripts/orca-vm/x-destroy.sh   # or `destroy: none`
  ```
  The scripts run on the **desktop** (the Orca host), from the repo root. The env vars
  `ORCA_RECIPE_ID` and `ORCA_VM_INSTANCE_ID` are available. Recipes appear under **Run on** only when
  the `environmentRecipes` entry is on the primary checkout's branch. Validate with
  `orca vm recipe doctor <id> [--provision]`.
- **Two connection modes**, chosen by the `create` output:
  1. **Orca-server mode**: the env runs
     `orca serve --port $P --project-root /abs/repo --pairing-address wss://... --recipe-json`, and
     `create` emits `{schemaVersion:1, pairingCode, projectRoot, userData:{resourceId,...}}`.
  2. **SSH mode**: `create` emits a `connection: {type:"ssh", host, port, username, identityFile, ...}`
     block and Orca dials in over SSH. Optionally `checkoutMode: provisioned-root` (schema v2).
- **Docker**: the bundled reference `references/docker-ssh.md` covers "Local Docker over SSH". You build
  an image with sshd, the tools and the agent CLI, `docker commit` an authenticated image, have `create`
  run `docker run -d -p 127.0.0.1::22 ...`, and have `destroy` run `docker rm -f`. So the agent shell
  does run inside a container, via SSH.
- **Docker Compose or devcontainer**: neither has native support. The docs and skill guides never
  mention `devcontainer` or `docker compose`. A per-worktree compose stack is possible only through the
  user's own recipe scripts, or through `orca.yaml` setup and archive scripts. Orca has no compose
  awareness.
- Agent auth inside environments: log in once interactively with the device-auth flow (for example
  `codex login --device-auth`), then snapshot or commit an image. Don't bind-mount the host's
  `~/.codex` or `~/.claude`.

## 7. Agent harnesses

([agents/supported.mdx](R/docs/site/content/docs/agents/supported.mdx))

- Orca "works with **any CLI agent**"; the picker "just launches a process in a terminal". The built-in
  list has about 40 entries, including Claude Code, Codex, OpenCode, Gemini, Cursor CLI, Copilot CLI,
  Amp, Aider, Goose, Droid, Kimi, Qwen, Crush, Pi and Devin.
- Integration depth varies. Claude Code and Codex are "deep" (usage tracking, account hot-swap, hooks);
  most others are "Auto-setup", some with hooks and status.
- **Discovery** works by probing known commands on the host PATH, after hydrating PATH from the login
  shell ([src/main/preflight/agent-detection.ts](R/src/main/preflight/agent-detection.ts),
  `KNOWN_TUI_AGENT_DETECTION_COMMANDS`). On a remote or serve host, detection uses the **server's** PATH,
  HOME and credentials, not the client's.
- **Permissions default is YOLO.** New launches pre-fill `--dangerously-skip-permissions` (Claude),
  `--dangerously-bypass-approvals-and-sandbox` (Codex), `--yolo` (Gemini and others). Switch it in
  Settings → Agents → Agent Permissions. The docs add: "A worktree is an isolated checkout, not a
  security sandbox". That fits orcasm's VM isolation rationale.
- **Auth**: agents use their own CLI logins on the host. Managed accounts:
  `orca-ide account add --agent claude|codex|opencode|devin` runs the agent's login in the terminal
  (Codex uses device auth) and registers the account. Run it from the server shell on headless hosts;
  it needs the runtime running. Orca also writes agent hooks (`{userData}/agent-hooks/endpoint.env`) so
  agents report status back ([agents/hooks-memory.mdx](R/docs/site/content/docs/agents/hooks-memory.mdx)).
- Skills: `orca-ide skills install --skill orca-cli --skill orchestration` installs Orca's agent skills.

## 8. SSH-remote mode (alternative architecture)

([ssh.mdx](R/docs/site/content/docs/ssh.mdx), [ways-to-run.mdx](R/docs/site/content/docs/ways-to-run.mdx))

- **SSH targets**: the laptop Orca owns the runtime. You add a host under Settings → SSH (with
  `~/.ssh/config` import, jump hosts and multiplexing). Worktrees on that host run `git worktree` and
  the agents remotely through a small **relay** Orca installs on first connect. Remote terminals need
  native `node-pty`, which Linux may compile on the host. PTYs survive the laptop closing. The editor,
  diff and UI stay local. Host-key checks follow `known_hosts`.
- **Remote Orca Server**: the VM owns the full runtime (`orca serve`). Clients are the desktop app,
  the web client or mobile. This is the multi-client option.
- For orcasm, the options compare as follows:
  - **(A)** `orca serve` in the VM plus the web client in the host browser. This is the plan; it needs
    the Electron backend and Xvfb per section 2.
  - **(B)** The host's Orca desktop pairs to `orca serve` in the VM. It needs no web client, but the
    host must run Orca.
  - **(C)** The host's Orca desktop uses the VM as an **SSH target**. Nothing Orca-specific is needed in
    the VM beyond git, node-pty build tools and the agent CLIs.
  - **(D)** A recipe in Docker-over-SSH style, where the microsandbox VM is the "environment" per
    worktree.

## Unknown / unverified

- **Does the orcad backend serve the web client?** Source reading says no (`webClientRoot` is not
  passed). This was not run. Test: `orca-ide serve --port 6768` and look for a `Web client URL:` line;
  compare with `ORCA_SERVE_RUNTIME=electron`.
- Whether Electron serve works in a minimal microsandbox VM: Xvfb auto-start, cgroup v2 or systemd user
  scopes for terminals, `/dev/shm`, and libraries Chromium needs (nss, gtk, ...). Not tested.
- How shared `orca.yaml` command **approval/trust** is granted headlessly, for example on
  `worktree create --setup run` from the CLI. The docs say a valid file "does not bypass Orca's command
  approval"; the mechanism and any pre-trust option were not found.
- No documented way to pre-seed global settings (workspaceDir, agent permissions, setup policy)
  non-interactively. `profile-state.db` is SQLite and undocumented.
- Whether the web client works over plain `http://` with non-loopback hosts, given secure-context
  requirements of browser APIs such as clipboard. With a loopback port-forward this is likely fine.
- Exact web-client feature gaps beyond Download and Keep-awake, such as file upload and the native
  dialogs needed for "add repo" folder picking.
- `serve service install` / `--supervise` exist only on `main`, after v1.4.227. Expect them in a coming
  release.
- Whether the `orca.dev` URL in the brief refers to this product: the official homepage per GitHub is
  onOrca.dev.

## Agent-in-container (per-worktree Docker)

Question: can each Orca worktree run its agent inside a Docker container dedicated to that worktree
(worktree bind-mounted), while agents on the main checkout run on the host? Source: same `main`
commit `3314659`. Paths are relative to the repo root.

### 1. `environmentRecipes` (orca.yaml)

- **One environment per workspace, created on demand.** "Each worktree can boot its own on-demand
  environment ... cloud sandbox, VM, or local Docker container" and "a fresh, recipe-defined sandbox
  that dies with the worktree" (`docs/site/content/docs/ways-to-run.mdx:73,118`). The provision IPC
  takes `workspaceName`/`workspaceId`/`branch`/`ref` for each call (`src/main/ipc/ephemeral-vm.ts:108-122`).
  So one recipe yields one container per worktree. The user picks it under **Run on** at create time.
- **Gated behind an experimental flag.** `experimentalEphemeralVms` defaults to false
  (`src/shared/global-settings-types.ts:507-508`, `src/shared/default-global-settings.ts:259`). The
  composer only probes recipes when it is on (`src/renderer/src/hooks/composer-state/runtime-target-selection.ts:252-253`).
  The UI label is Settings → Experimental → Cloud VM (`ways-to-run.mdx:76,94`).
- **Desktop-local only (inferred).** Recipes load only for git repos on the local execution host with
  no SSH `connectionId` (`src/renderer/src/hooks/useEphemeralVmRecipeOptions.ts:25-31`). They load
  through the Electron preload `window.api.ephemeralVm` (`:44`), whose handlers are `ipcMain` handlers
  (`src/main/ipc/ephemeral-vm.ts:69-77`). No runtime-RPC equivalent was found, and the CLI has only
  `vm recipe doctor` (`src/cli/specs/vm.ts:7`). So recipes most likely do not work from the web client
  of a headless `orca serve`, nor for repos owned by a paired server. Not run.
- **Recipe shape**: `id`, `name`, `create` (required), optional `suspend`/`resume`, and `destroy` (or
  `destroy: none`) (`skill-guides/orca-per-workspace-env.md:278-291`). `create` runs locally from the
  repo root and prints one JSON object on stdout. Progress output goes to stderr (`:198-200`, `:288`).
- **Script environment** (`src/shared/ephemeral-vm-recipe-process.ts:230-252`): the parent env plus
  `ORCA_VM_MODE`, `ORCA_VM_INSTANCE_ID`, `ORCA_RECIPE_ID`, `ORCA_PROJECT_ID`, `ORCA_WORKSPACE_ID`,
  `ORCA_WORKSPACE_NAME`, `ORCA_REPO_PATH` (the host repo), `ORCA_REPO_URL`, `ORCA_REPO_BRANCH`,
  `ORCA_REPO_REF`, `ORCA_REPO_REF_HEAD`, `ORCA_RECIPE_RESULT_SCHEMA_VERSION` and `ORCA_VERSION`.
  There is **no worktree path**. The worktree does not exist yet; Orca creates it inside the
  environment afterwards. `suspend`/`resume`/`destroy` get a lifecycle JSON payload on stdin, including
  `recipeResult.userData.resourceId` (`orca-per-workspace-env.md:262-267`).
- **Two connection modes** (`orca-per-workspace-env.md:34-36`):
  - **Orca-server mode**: `create` starts `orca serve --port --project-root --pairing-address
    --recipe-json` inside the environment and returns `{pairingCode, projectRoot}` (`:293-337`).
  - **SSH mode**: `create` returns `{schemaVersion:1, connection:{type:"ssh", projectRoot,
    target:{label,host,port,username, identityFile?, identitiesOnly?, jumpHost|proxyCommand,
    portForwards?, relayGracePeriodSeconds?}}}`. Orca then dials in, installs its relay or managed
    server, and "imports the repo" (`src/cli/bundled-skill-guides.ts:69`, the ssh-host reference).
    The provision result for SSH carries an `sshTargetId` and "the managed Orca server the host runs;
    its workspace lives there" (`src/main/ipc/ephemeral-vm.ts:51-61`).
- **A local Docker reference recipe exists** (`bundled-skill-guides.ts:60`, docker-ssh reference).
  The container runs `sshd`, published with `-p 127.0.0.1::22`. Each container generates its own host
  keys, and the script records them in the desktop `known_hosts`. The agent auth is baked in with
  `docker commit` of an auth container. `destroy` runs `docker rm -f`. The reference says not to
  bind-mount the host agent home.
- **What must exist in the target**: `sshd`, `git` and a clone at `projectRoot`. Orca ships a pinned
  Node runtime that needs glibc ≥ 2.28, or 2.17 via a compat build (`src/main/ssh/pinned-runtime-host-target.ts:1-16`),
  so Alpine/musl is likely out. `make`, a C++ compiler and `python3` are needed for `node-pty`, or
  remote terminals fail (`docs/site/content/docs/ssh.mdx:87-96`). The agent CLI must be installed and
  logged in.
- **Mismatch with the bind-mount idea.** In SSH mode the git worktree is created **on the remote**
  (`ssh.mdx:34`). The container owns the checkout, and Orca's editor and diff go through the relay. A
  host worktree bind-mounted into a container is not the model. One could bind-mount the host repo as
  `projectRoot`, but Orca would then run `git worktree add` inside the container. This is untested.
- Recipes are themselves a trust-gated script kind (`vmRecipe`;
  `src/renderer/src/lib/ensure-hooks-confirmed.ts:48-66,297-299`).

### 2. How agents are launched; does a PATH shim work?

- **Terminal agents**: the PTY spawns the user's shell, and Orca **types the startup command** once
  the shell reports ready (`src/main/providers/local-pty-shell-ready-startup-command.ts:1-3,15-21`).
  The command is the catalog binary name (`claude`, `codex`, `opencode`;
  `src/shared/tui-agent-config.ts:28-35,64,92`) or a **global per-agent override**, `agentCmdOverrides`
  (`src/shared/global-settings-types.ts:408-409`, `src/shared/tui-agent-launch-command.ts:35-40`).
  It is resolved through the shell's PATH, so a `claude` shim on PATH, or an override pointing at a
  wrapper, is picked up. The override is global, not per repo, so the wrapper must decide
  host-vs-container from `$PWD`.
- **What breaks in the container unless forwarded**:
  - **Status hooks**: the PTY env gets `ORCA_PANE_KEY`, `ORCA_TAB_ID`, `ORCA_WORKTREE_ID` and
    `ORCA_AGENT_HOOK_{PORT,TOKEN,ENV,VERSION,TRANSPORT,ENDPOINT}`
    (`src/main/runtime/terminal/launch/terminal-workspace-scope.ts:196-205`,
    `src/main/runtime/orca-runtime-core.ts:276-283`). The hook scripts `curl -X POST
    http://127.0.0.1:${ORCA_AGENT_HOOK_PORT}/hook/<agent>` (`src/main/agent-hooks/hook-post-command.ts:14,24`).
    The scripts live at `~/.orca/agent-hooks/` (`src/main/agent-hooks/installer-utils.ts:112-114`).
    For Claude they are registered in `~/.claude/settings.json` (`src/main/claude/hook-settings.ts:50-51`).
    Inside a container, all of these are missing: the env vars (unless `docker exec -e`), the script
    path, the settings entry, and host loopback (unless `--network host`). The result is **no
    working/waiting/done status** from hooks.
  - **Codex home**: Orca sets `CODEX_HOME`/`ORCA_CODEX_HOME` to an Orca-managed home under userData
    (`src/main/ipc/pty/host-env/assembly.ts:245-251`, `src/main/codex/codex-home-paths.ts:41`), where it
    writes `hooks.json` (`src/main/codex/codex-hook-definition.ts:33-35`). That is a host path.
  - **Process inspection**: status also reads the PTY's foreground process
    (`src/main/runtime/runtime-pty-foreground-agent.ts:41`) and recognizes agents by name or by
    command line, including interpreter + script (`src/shared/agent-process-recognition.ts:180-217`).
    A shim that `exec`s `docker` shows up as `docker`. A non-`exec` bash wrapper named `claude` is
    probably recognized (inferred).
  - **Terminal-title (OSC) status** passes through `docker exec -t` unchanged
    (`src/shared/agent-title-status.ts:297`). This is the one signal that survives for free.
  - **Folder trust preflight**: Orca pre-trusts the worktree in the host agent config
    (`preflightTrust`, `src/main/agent-workspace-trust-spawn.ts:40-47`). The container's agent config
    will not have it unless the path and config match.
  - **Structured (chat) sessions** spawn Claude through the Agent SDK or the Codex app-server as direct
    children (`src/main/claude/claude-agent-sdk-process-spawn.ts`), not through the shell. Shim
    behavior there was not checked.
- **Verdict**: a PATH shim *launches* fine, but Orca's agent integration (status hooks, Codex home,
  trust) assumes the agent runs on the same host as Orca's runtime. The supported way to run agents
  elsewhere is SSH or a recipe, where Orca installs hooks remotely and relays them
  (`src/main/codex/codex-hook-remote-install.ts`; `ssh.mdx:48` says status works over SSH).

### 3. `scripts.setup`/`archive` approval

- **Desktop/web renderer path**: before running shared `orca.yaml` setup (including `defaultTabs`
  commands), archive, issueCommand or vmRecipe, the renderer hashes the content and compares it with
  `trustedOrcaHooks[repoId][kind].contentHash`. Repo-wide `trustedOrcaHooks[repoId].all` skips the
  check. Otherwise it opens the `confirm-orca-yaml-hooks` modal (`src/renderer/src/lib/ensure-hooks-confirmed.ts:103-157,240-330`).
  `local-only` command source bypasses the prompt (`:284`). Trust is stored in **persisted UI
  state** (`src/shared/persisted-ui-state-types.ts:228`, shape in
  `src/shared/orca-yaml-hook-types.ts:64-79`) and is set through the client UI RPC
  (`src/shared/rpc-contract/client-ui-params.ts:232`).
- **Runtime/CLI path** (`orca-ide worktree create --setup run|skip|inherit`) **does not check the hash
  trust**. `prepareRuntimeLocalWorktreeSetup` decides only from `setupDecision` and the repo's
  `setupRunPolicy` (`src/main/runtime/runtime-local-worktree-setup.ts:11-15,37-60`,
  `src/main/effective-hook-config.ts:64-78`). `run` always runs. `inherit` throws if the policy is
  `ask`. `defaultTabs` commands follow the same decision and are suppressed by `local-only`
  (`effective-hook-config.ts:94-115`). So **headless pre-grant = create via CLI with `--setup run`**,
  or set the repo's setup policy to `run-by-default` (the default, `src/shared/constants.ts:145`).
  Archive needs `--run-hooks` (`docs/site/content/docs/model/orca-yaml.mdx:102`).
- No CLI flag was found to write `trustedOrcaHooks`. It could only be pre-seeded by editing persisted
  UI state (undocumented).

### 4. Per-worktree default tabs

- **Yes.** `defaultTabs: [{title, command?, color?}]` in the **new worktree's** `orca.yaml` creates
  tabs once at creation (`orca-yaml.mdx:21-24,42,67,106-108`). For example:
  `- title: Container shell` / `command: docker compose exec workbench bash`.
- Caveats: in the desktop composer the **first** tab is taken by the agent and its command is not run.
  CLI `--agent` instead adds a separate terminal (`orca-yaml.mdx:30,108`). Tab commands **do not wait
  for setup** (`:108`), so a `docker compose up` in setup can race them; wrap the command in a wait
  loop. Tab commands go through the same setup decision and trust prompt as setup
  (`ensure-hooks-confirmed.ts:34-46`).
- Setup scripts get `ORCA_ROOT_PATH`, `ORCA_WORKTREE_PATH` and `ORCA_WORKSPACE_NAME`
  (`orca-yaml.mdx:91-98`). These are enough for `scripts.setup` to start a per-worktree compose
  project (`-p "$ORCA_WORKSPACE_NAME"`) with the worktree bind-mounted, and for `scripts.archive` to
  tear it down. This route (setup + archive + defaultTabs + an agent shim) is the non-experimental
  alternative to recipes. It gives up hook-based agent status unless the hooks are forwarded.

---

## Spike results (2026-10-16)

This was a throwaway hands-on spike on the Linux host (Fedora, `msb` 0.7.8, KVM). It used one sandbox, `orcasm-spike-1`, labelled `orcasm-spike=1`, which was removed afterwards. Nothing remains. Pairing codes in the logs were redacted.

### 1. Sandbox

```sh
msb create ghcr.io/superradcompany/ubuntu-systemd:24.04 --name orcasm-spike-1 --label orcasm-spike=1 \
  --init auto -c 4 -m 8G --root-disk 16G -p 127.0.0.1:16768:6768
```

- The create command returned in about 0.5 s, because the image was already cached for `oc-dev`.
- The guest had systemd as PID 1, `nproc` 4, 7.9 GiB RAM and a 16G overlay `/`. The kernel was 6.12.111.
- The guest had outbound internet. GitHub downloads ran at about 100 MB/s.

### 2. Installing Orca v1.4.227 (.deb)

- Download the `.deb` inside the guest:
  `curl -fsSLO https://github.com/stablyai/orca/releases/download/v1.4.227/orca-ide_1.4.227_amd64.deb`
  It is 203 MB.
- Install it with `apt-get install -y --no-install-recommends ./orca-ide_1.4.227_amd64.deb`. This took 44 s and set up 161 packages.
- The `.deb` declares `Depends: libgtk-3-0, libnotify4, libnss3, libxss1, libxtst6, xdg-utils, libatspi2.0-0, libuuid1, libsecret-1-0, python3, python3-gi, gir1.2-atspi-2.0, at-spi2-core, xdotool, xclip, xvfb`. **Xvfb therefore comes with the package.**
- **Missing dependency:** `libasound2t64` is not declared. Without it, even `orca-ide --version` fails with `libasound.so.2: cannot open shared object file`. The CLI is the Electron binary running with `ELECTRON_RUN_AS_NODE=1`.
  - Full extra install: `apt-get install -y --no-install-recommends ./orca.deb libasound2t64`.
- Footprint: `/opt/Orca` is 662 MB. Root disk use after install was 1.3 G.
- The `.deb` installs `/opt/Orca/chrome-sandbox` setuid root, mode `-rwsr-xr-x`.

### 3. Default (orcad) backend: **broken in the packaged .deb, and it has no web client anyway**

- `orca-ide serve --port 6768 --pairing-address 127.0.0.1:16768`, run as root, printed:
  ```
  Error: Cannot find module '/opt/Orca/resources/app.asar.unpacked/out/main/orcad/orcad-local-serve-selection-entry.js'
  [serve] using Electron serve: the app did not answer which serve host to use (exit 1)
  FATAL:electron_main_delegate.cc:225] Running as root without --no-sandbox is not supported.
  ```
- **Packaging bug in v1.4.227:**
  - `serve-orcad-launch.js` builds the selector path from `resolveAppRoot()`, which is `resources/app.asar.unpacked`.
  - The selector entry ships only inside `resources/app.asar`.
  - So packaged Linux `orca serve` **always falls back to Electron**.
- Running the selector by hand works:
  ```sh
  ELECTRON_RUN_AS_NODE=1 /opt/Orca/orca-ide /opt/Orca/resources/app.asar/out/main/orcad/orcad-local-serve-selection-entry.js --user-data /root/.config/Orca --app-root /opt/Orca/resources/app.asar
  ```
  It printed `ORCA_SERVE_RUNTIME {"kind":"orcad",...}` after downloading a pinned Node into `<userData>/orcad-artifacts/` (about 2 s).
- Launching orcad the way the CLI would:
  ```sh
  ORCA_USER_DATA=... node orcad-server.js --bind 0.0.0.0 --port 6768 --pairing-address 127.0.0.1:16768
  ```
  It printed `Orca server ready`, `Bound endpoint: ws://0.0.0.0:6768`, `Advertised endpoint: ws://127.0.0.1:16768` and a `Pairing URL: orca://pair?...`.
  - **It did not print a `Web client URL` line.** This confirms the source reading: orcad has no `webClientRoot`.
- Results of requests against orcad:

  | Request | Result |
  |---|---|
  | `curl http://127.0.0.1:16768/web-index.html` from the host | Hangs (timeout, 0 bytes) |
  | The same request from inside the guest on `:6768` | Hangs |
  | WebSocket upgrade (`curl -H 'Upgrade: websocket' ...`) from the host | `HTTP/1.1 101 Switching Protocols` |

  So the port publish works, but orcad serves no static files.
- orcad also logged `Installed Electron browser provider unavailable` and `[agent-state-rules] download failed: HTTP 404`. Neither stopped it.

### 4. Electron backend: **works, but it must run as a non-root user**

- Electron refuses to run as root without `--no-sandbox`.
  - The CLI adds `--no-sandbox` only for an extracted AppImage (`shouldDisableExtractedAppImageSandbox`).
  - There is no environment knob to add it for the `.deb`.
- Running as a normal user works with the setuid `chrome-sandbox`:
  ```sh
  useradd -m -s /bin/bash orca
  msb exec orcasm-spike-1 -u orca -w /home/orca -- bash -c \
    'ORCA_SERVE_RUNTIME=electron nohup setsid orca-ide serve --port 6768 --pairing-address 127.0.0.1:16768 > ~/serve-electron.log 2>&1 &'
  ```
  Output:
  ```
  Orca server ready
  Bound endpoint: ws://0.0.0.0:6768
  Advertised endpoint: ws://127.0.0.1:16768
  Web client URL: http://127.0.0.1:16768/web-index.html#pairing=<REDACTED>
  Pairing URL: orca://pair?<REDACTED>
  ```
- Orca **started `Xvfb :99 -screen 0 1280x1024x24 -nolisten tcp -terminate` by itself**. The GPU process used `--ozone-platform=x11`.
- On first run it installed `~/.local/bin/orca-ide` and `~/.local/bin/orca`. userData is `~/.config/orca`.
- Harmless noise in the log:
  - D-Bus session bus errors.
  - `secrets are stored unencrypted`, because there is no keyring.
  - A login1 Inhibit permission denial.
- Requests from the **host**:

  | Request | Result |
  |---|---|
  | `curl http://127.0.0.1:16768/web-index.html` | `HTTP/1.1 200 OK`, `Content-Type: text/html`, 4605 bytes, `<title>Orca Web</title>` |
  | `/assets/web-B7Yx-qEs.js` | 200, 191 KB |

- Memory: about 700 MiB RSS for all of the `orca` user's processes, Xvfb included, while idle. Guest "used" memory was 478 MiB.
- Packages needed for Electron serve: only the `.deb` plus its declared Depends (which include `xvfb`), and **`libasound2t64`**. Nothing else was needed for serve to start and serve the web client. Not tested: the server-side browser screencast, and an actual browser pairing session.

### 5. Guest bind address and msb publishing

The test used `python3 -m http.server 6768` in the guest, with the published `127.0.0.1:16768 -> 6768`:

| Guest listener bound to | Host `curl 127.0.0.1:16768` |
|---|---|
| `--bind 127.0.0.1` | `curl: (52) Empty reply from server` |
| `--bind 0.0.0.0` | `hello-0.0.0.0` (works) |

**The guest service must bind 0.0.0.0** (or the guest NIC). `orca serve` already does this with both backends.

### 6. `msb ssh serve --stdio` as an ssh ProxyCommand: **works**

```sh
ssh -F /dev/null -o IdentityAgent=none -o IdentitiesOnly=yes -i spike_key \
  -o "ProxyCommand=msb ssh serve orcasm-spike-1 --stdio" root@orcasm-spike 'id -un; hostname'
# -> root / orcasm-spike-1  (rc 0)
```

- Before `msb ssh authorize` the login fails with `Permission denied (publickey,...)`.
- `msb ssh authorize --file key.pub` **appends to the global `~/.microsandbox/ssh/authorized_keys`**. It is not per sandbox, so every sandbox accepts the key.
- After the test, the throwaway key line was removed. The file's sha256 matches the pre-spike backup. Access was revoked immediately, with no restart: the next login was `Permission denied`.
- The SSH host key seen was ED25519.

### Implications for orcasm

- Use the `.deb`, plus `libasound2t64`, and run `ORCA_SERVE_RUNTIME=electron orca-ide serve --port 6768 --pairing-address 127.0.0.1:<hostport>` **as a non-root user**. A systemd unit with `User=` would do this. Set `ORCA_SERVE_RUNTIME=electron` explicitly so a future fix of the orcad selector does not silently switch to the web-client-less orcad.
- Publish with `-p 127.0.0.1:<hostport>:6768`. The host's own Orca already listens on `0.0.0.0:6768`, so the host port must differ.
- Read the `Web client URL` from the serve log. It contains the pairing secret in the fragment.
- SSH works for editors and tools through `ProxyCommand msb ssh serve NAME --stdio`. Key authorization is global to `MSB_HOME`.

## Hooks without committed orca.yaml / multi-repo

Source: `stablyai/orca` main @ `3314659c`. Paths are relative to the repo root.

### 1. Where `orca.yaml` is read from

`loadHooks(path)` reads only `<path>/orca.yaml` from disk. It does not use git, so tracked status does not matter (`src/main/hooks.ts:119-131`). The real question is which `path` each caller passes:

| Moment | Path read | Citation |
|---|---|---|
| Pre-create `ask`-policy gate | **main checkout** (`repo.path`) | `src/main/runtime/runtime-local-worktree-create.ts:87`, `src/main/ipc/worktree-remote.ts:1135` |
| **Setup** script + `defaultTabs` | **new worktree** (`worktreePath`) | `src/main/runtime/runtime-local-worktree-setup.ts:37-38`, `src/main/ipc/worktree-remote.ts:1650-1656` ("the worktree's base-branch `orca.yaml` is authoritative") |
| **Archive** script | **main checkout** (`repo.path`; `hooksPath` undefined) | `src/main/ipc/worktrees/removal/execute-worktree-removal.ts:151-166`, `src/main/runtime/runtime-registered-local-worktree-removal.ts:83-95`, `hooks.ts:167-170` |
| `worktree.sharedDirectories` | main checkout | `src/main/git/worktree-shared-directories.ts:41,93` |

There is no fallback from the worktree to the root. Consequences for an uncommitted or gitignored `orca.yaml` in the main checkout:

- **archive**: honored.
- **setup**: **not** honored, because the new worktree does not contain the file. There is one exception. Before setup is evaluated, Orca copies `.worktreeinclude` entries and symlinks `repo.symlinkPaths` from the main checkout into the new worktree (`worktree-remote.ts:1610-1648` comes before `:1652`; runtime path `src/main/runtime/runtime-local-worktree-materialization.ts:152,174`). Both lists are read from disk in the main checkout (`src/main/git/worktree-include-file.ts:58-69`), so neither needs a commit. So `symlinkPaths: ["orca.yaml"]`, set per repo through `repo.update`, makes an untracked root `orca.yaml` reach setup. It is a workaround, and it leaves an untracked symlink in each worktree.

### 2. Per-repo hook settings outside `orca.yaml`: yes, and this is the clean answer

- `Repo.hookSettings: { setupRunPolicy?, commandSourcePolicy?, setupAgentStartupPolicy?, scripts: { setup, archive } }` is stored in Orca's own repo state (`src/shared/repo-types.ts:72`, `src/shared/orca-yaml-hook-types.ts:51-62`). This is the "Repository settings → hooks" UI (`src/renderer/src/components/settings/RepositoryHooksSection.tsx:149-205`).
- Merging: `getEffectiveHooksFromConfig` combines yaml and local scripts according to `commandSourcePolicy`: `shared-only`, `local-only` or `run-both` (`src/main/effective-hook-config.ts:12-58`). If the policy is **unset and a local script exists, it resolves to `local-only`** (`src/shared/hook-command-source-policy.ts:11-13`). So setting only `scripts` is enough. Local scripts apply at every row of the table above, because they come from `repo`, not from a path. They run with `ORCA_ROOT_PATH`, `ORCA_WORKTREE_PATH` and `ORCA_WORKSPACE_NAME` (`src/main/setup-hook-env-vars.ts`).
- Trust: the renderer's yaml-trust modal is skipped for `local-only` (`src/renderer/src/lib/ensure-hooks-confirmed.ts:277-284`). The runtime/CLI path never checks trust (see "`scripts.setup`/`archive` approval" above). The setup run still follows `setupRunPolicy`, which defaults to `run-by-default`.
- **Headless setting**: the RPC `repo.update` (permission `workspace`) accepts `updates.hookSettings` (unvalidated `z.unknown()`), `symlinkPaths` and `worktreeBasePath` (`src/shared/rpc-contract/repo-update-params.ts:55-59`, `src/main/runtime/rpc/methods/repo.ts:175-188`, `src/main/runtime/runtime-repository-settings-controller.ts:76-105`).
  - The `orca-ide repo set` CLI only exposes `--external-worktree-visibility` (`src/cli/handlers/repo.ts:26-40`, `src/cli/specs/repo.ts:27`). **There is no CLI flag for hooks.**
  - To call the RPC directly, use the same transport the CLI uses. Read `<userData>/orca-runtime.json` (`src/shared/runtime-bootstrap.ts:45-48`) to get the unix-socket endpoint and `authToken`. Then send one newline-terminated JSON line, `{id, authToken, method:"repo.update", params:{repo:"path:/…", updates:{hookSettings:{mode:"auto", setupRunPolicy:"run-by-default", scripts:{setup:"…", archive:"…"}}}}}` (`src/cli/runtime/transport.ts:199-213`, `src/cli/runtime/metadata.ts:11-30`). A paired WebSocket client can call the same method. Pass the full `hookSettings` object, since it replaces the old one. Hydration fills in defaults (`src/main/persistence/tracking-repos/repo-hydration.ts:84-89`).
- There is no `orca.local.yaml` or global hook override. The only names `loadHooks` knows are `orca.yaml` (`hooks.ts:120`). `.orca/issue-command` is a per-user override for `issueCommand` only (`src/main/issue-command-file.ts:1`).

### 3. Worktree location

- The global setting `workspaceDir` defaults to `~/orca/workspaces` (`src/shared/constants.ts:104-112`). With `nestWorkspaces` set, the path is `<root>/<repoName>/<name>`; otherwise it is `<root>/<name>` (`src/main/ipc/worktree-logic.ts:119-131`).
- Per repo, `Repo.worktreeBasePath` overrides `workspaceDir`. A relative value resolves from the repo path (`repo-types.ts:70-71`, `worktree-logic.ts:221-234,270-284`).
- Setting it headlessly:
  - **per repo**: the `repo.update` RPC (`updates.worktreeBasePath`), or the CLI `orca-ide project setup-create|setup-update --worktree-base-path` (`src/cli/handlers/project.ts:170,192`).
  - **global**: `workspaceDir` is **not** in the `settings.update` RPC schema (`src/shared/rpc-contract/client-settings-params.ts:87+`). It would have to be pre-seeded in persisted state, which is undocumented.
- `worktree create` has no `--path` flag (`src/cli/specs/core.ts:70`). The `nestWorkspaces` override exists only as an RPC param (`src/shared/rpc-contract/worktree-create-params.ts:62`).

### 4. "Attached worktrees" / multi-repo concept

- **Project groups** (`src/shared/project-group-types.ts:1-18`; RPC `projectGroup.list/create`, `src/main/runtime/rpc/methods/repo.ts:48-54`) group several repos (`Repo.projectGroupId`, `repo-types.ts:119`). A group can be imported from a folder scan of a parent directory (`createdFrom: 'folder-scan'`).
- A **folder workspace** (`src/shared/folder-workspace-types.ts:18-47`; RPC `folderWorkspace.create {projectGroupId, folderPath?}`, `src/main/runtime/rpc/methods/folder-workspace.ts:20-29`) is a workspace rooted at a plain folder, typically the group's parent dir. It is not a git worktree.
- **Attached worktrees** are worktrees in any repo whose `WorkspaceLineage.parentWorkspaceKey` is `folder:<id>` (`src/shared/worktree/lineage-types.ts:33-45`). The right-sidebar panel resolves them in `src/renderer/src/components/right-sidebar/folder-workspace-attached-worktrees.ts:28-80`. They are created with `orca worktree create --repo … --parent-worktree folder:<id>` (`src/cli/specs/core.ts:105,124`), or attached afterwards with `orca worktree set --parent-worktree …` (`src/cli/specs/worktree-set.ts:8`).
- Worktree-to-worktree lineage (`parentWorktreeId`) also exists. No same-repo restriction was found in the selector code (`src/main/runtime/runtime-worktree-lineage-resolution.ts:59,80`).
- Folder workspaces run **no** setup or archive hooks: `runHook` is only called from the worktree create and remove paths listed in §1.
- So Orca's native multi-repo model is: a group folder workspace, with per-repo worktrees attached to it, each with its own hooks. It is not "one worktree that owns sibling worktrees".

### 5. Can a setup script create git worktrees in other repos?

- Orca only discovers worktrees of **registered** repos, via `git worktree list --porcelain` on each repo (`src/main/git/worktree-list-reader.ts:224`). A worktree of an unregistered repo is invisible to Orca.
- If the other repo **is** registered, an `add` done outside Orca is classified by `classifyWorktreeOwnership` (`src/shared/worktree/ownership.ts:111-158`). Without Orca metadata it is `external`, `unknown-legacy` or `agent-scratch`, never `orca-managed`. For repos registered after the visibility rollout (`externalWorktreeVisibilityLegacy: false`, `src/main/runtime/runtime-repository-registration-controller.ts:81`), these worktrees are **hidden by default** (`src/shared/worktree-visibility-resolution.ts:24-55`, `src/shared/external-worktree-visibility.ts:66-78`). They can be shown with `repo set --external-worktree-visibility show` or by `importedExternalWorktreePaths`.
- If the script places a sibling worktree under a configured `worktreeBasePath`, it is classified as `external` (`ownership.ts:138-145`), and it still stays hidden by default.
- Conflict risk: archiving runs the archive hook **before** any removal, as a hard precondition (`execute-worktree-removal.ts:153-170`). Removal then runs `git worktree remove [--force] <path>` (`src/main/git/worktree-removal.ts:77-84`). Do **not** nest sibling worktrees inside the primary worktree directory, or a forced remove deletes them with it. Keep siblings elsewhere and let the archive hook `git -C <other> worktree remove` them.
- Hooks time out after 2 minutes (`hooks.ts:24`). When the hook runs in-process this is a hard limit; the desktop path instead launches setup as a visible terminal runner (`worktree-remote.ts:1683-1695`).
