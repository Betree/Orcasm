# microsandbox: research notes for orcasm

Researched 2026-10-16 against `msb` 0.7.8 (installed) and repo `main` @ `c916e2d` (2026-10-16).

Sources: `msb <cmd> --help` output (0.7.8), the docs at <https://docs.microsandbox.dev> (source in the repo under `docs/`), and the repo itself. `github.com/microsandbox/microsandbox` redirects to **`github.com/superradcompany/microsandbox`** (Apache-2.0). Docs links below are written as `docs:/path`, which means `https://docs.microsandbox.dev/path`.

Everything here comes from docs, help text or source. **Nothing in §1–10 was tested by creating a sandbox** (sandboxes were off-limits for this research). §11 was tested in throwaway sandboxes. One extra data point: the user's existing sandbox `oc-dev` was inspected with the read-only `msb inspect` command. It is already close to the orcasm shape: image `ghcr.io/superradcompany/ubuntu-systemd:24.04`, 8 vCPU, 45 GiB RAM, 32 GiB managed ext4 root, a host bind at `/workspace`, an owned 100 GiB ext4 disk at `/var/lib/docker`, and custom labels.

---

## 1. Version, platforms, install

- **Installed:** `msb 0.7.8`, from Homebrew (`superradcompany/tap/microsandbox`). Latest release is v0.7.8 (2026-10-09). `msb doctor` on this host reports Linux x86_64, libkrunfw 5.6.1, KVM read/write OK, reflink OK.
- **Runtime:** libkrun microVMs. Each sandbox is a child VM process; there is **no daemon** (docs:/getting-started/quickstart, docs:/sdk/overview).
- **Platforms:**
  - **Linux:** x86_64 and ARM64. Needs glibc ≥ 2.28 (musl-only hosts are unsupported) and read/write access to `/dev/kvm` (usually through the `kvm` group). Inside a cloud VM or CI runner, the outer hypervisor must expose nested virtualization (docs:/troubleshooting/linux).
  - **macOS:** Apple Silicon only. Intel Macs are not supported, even under Rosetta. No extra setup is needed (docs:/troubleshooting/macos).
  - **Windows:** preview only. Needs Windows 10+ on x64 or ARM64, with the **Windows Hypervisor Platform** feature enabled (`HypervisorPlatform`, which is separate from `VirtualMachinePlatform`) (docs:/troubleshooting/windows).
- **Install:**
  - macOS and Linux: `curl -fsSL https://install.microsandbox.dev | sh`
  - Windows: `irm https://install.microsandbox.dev/windows | iex`
  - Without installing: `npx microsandbox ...`
  - Homebrew tap: `superradcompany/tap/microsandbox`
  - SDK packages can also bundle the runtime (docs:/getting-started/quickstart, docs:/sdk/setup).
  - Maintenance commands: `msb doctor [--fix]`, `msb self update|downgrade|uninstall`.

## 2. Lifecycle, naming, labels

- **Commands:**
  - Create and start: `msb create [IMAGE] --name N` boots the sandbox detached. `msb run` creates it and runs the image command (`-d` to detach). `msb start`, `stop [--force] [-t secs]`, `restart`, `pause`/`resume` (stays resident in RAM), `wait`, `ping`, `touch`.
  - Remove: `msb rm [-f]`.
  - Inspect: `msb ls|ps|inspect|logs|metrics`.
  - Edit: `msb modify`.
  - Force-kill a sandbox with **`msb stop --force`**. Note that `msb kill` does not kill a sandbox; it signals a *managed job* (`--help`; docs:/sandboxes/lifecycle).
- **Persistence:**
  - **Stop** keeps the configuration and filesystem. **Start** boots a fresh VM, so processes do not survive a stop/start.
  - **Pause** keeps the processes and RAM.
  - **Remove** deletes:
    - the sandbox record and its labels
    - the managed writable root (`upper.ext4`)
    - logs
    - owned volumes
  - **Remove** keeps named volumes, bind-mounted host data, snapshots, cached images, and user-supplied disk images (docs:/sandboxes/lifecycle "Destroy or remove").
- **Run behaviour:** unnamed `msb run` sandboxes are removed when the command finishes; named ones persist (docs:/cli/sandbox-commands).
- **Naming:**
  - `-n/--name`, up to 128 UTF-8 bytes. Without a name, msb generates one like `msb-xxxxxxxx` (`crates/cli/lib/ui.rs`).
  - `--replace` / `--replace-with-timeout` recreates a sandbox under an existing name.
  - SDKs offer `connect_or_create`. Creation options apply **only** to new sandboxes; an existing sandbox keeps its saved configuration (docs:/sandboxes/lifecycle).
- **`:name` syntax:** there is no `:name` sandbox addressing. The colon forms that exist are:
  - `SANDBOX:/abs/path` in `msb cp`
  - `group:member` for snapshots, e.g. `msb snap restore app:ready`
  - `SRC:DEST[:OPTS]` for mounts
- **Labels: yes.**
  - Set them with `--label KEY[=VALUE]` (repeatable) at create time. Change them live with `msb modify N --label k=v` or `--label-rm k`.
  - Filter and bulk-act with `--label` on `ls`, `ps`, `start`, `stop`, `restart`, `ping`, `touch` and `rm`. Several `--label` flags must all match (AND).
  - A bare `--label orcasm` is stored as `orcasm=""`. The selector parser maps a bare key to an empty value (`parse_label_selectors` in `crates/cli/lib/commands/common.rs`), so `msb ls --label orcasm` should match it. It ran without error here and returned no results.
  - Reserved key prefixes: `sandbox.`, `microsandbox.`, `service.`. Image `LABEL`s are imported automatically (docs:/sandboxes/labels).
  - Recommendation: use an explicit value, such as `--label orcasm=1` or `--label app=orcasm`, to avoid any ambiguity with empty-value matching.
- **Automatic lifecycle:** `--max-duration` and `--idle-timeout`.

## 3. Images and provisioning

- **Images:** any OCI image from Docker Hub, GHCR, ECR, GCR or another registry. Tags are **pinned to layers at first pull**. msb also boots disk images (qcow2/raw/vmdk). It can pull private registries and load local tarballs: `msb load` accepts Docker and OCI archives, and `msb save` exports them (docs:/images/overview).
- **Root filesystem layout:**
  - **Default (layered):** shared EROFS lower layers, plus a per-sandbox writable ext4 upper, combined by guest OverlayFS.
  - **Flat (alternative):** `--root-disk flat:SIZE[,clone=auto|copy|reflink]` gives a private ext4 clone mounted directly as root (docs:/sandboxes/bootstrap, docs:/sandboxes/optimization).
  - The writable root **persists across stop/start** and is deleted on `rm`.
- **Provisioning:**
  - Use a custom image, which is the recommended route for a reproducible Orca + Docker + Compose base.
  - Or provision on first boot with `msb exec`. The official agent guides `apt-get install` and `npm install -g` inside the guest.
  - Pre-boot patches: `--copy`, `--copy-dir`, `--copy-file`, `--mkdir`, `--rm`.
  - Named scripts: `--script NAME=BODY`, `--script-path`. They are placed in `/.msb/scripts`, which is on `PATH`.
  - Whole configuration in a file: `--conf FILE` (sandbox YAML) (`msb create --help`; docs:/cli/configuration).
- **systemd as PID 1:** `--init auto` with a systemd base such as `ghcr.io/superradcompany/debian-systemd:12` or `ubuntu-systemd:24.04` (from the `superradcompany/guest-images` repo). Then `systemctl enable --now` works. By default agentd is PID 1 (docs:/how-to/guest/systemd-services).
- **Snapshots:**
  - Disk snapshots: `msb snap create NAME --sandbox S`.
  - Full snapshots, including memory: add `--full`.
  - Export and import: `msb snap export` / `msb snap import` (`.msb` archives).
  - Restore into a new sandbox: `msb snap restore`.
  - A ready-built base sandbox could therefore be snapshotted and cloned.

## 4. Resources

- **Defaults:** 1 vCPU, 512 MiB RAM (docs:/sandboxes/overview).
- **Flags:** `-c/--cpus N`, `-m/--memory 4G`, `--max-cpus`, `--max-memory`. The `--max-*` values are boot-time ceilings that allow live growth.
- **Disk:** `--root-disk 32G` for a managed root, or `flat:32G`, `tmpfs:2G`, or a path to an image.
- **Changing resources later:** `msb modify N --cpus/--memory/--root-disk`.
  - CPU and memory change live, within the ceilings. Raising a ceiling needs a restart.
  - The root disk can **only grow**.
  - (docs:/sandboxes/tuning)
- **Default root disk size:** not found in the current docs. The v0.7 migration used 4 GiB as the legacy default (`crates/migration/lib/m20260527_000001_migrate_oci_rootfs_source.rs`). Set it explicitly.
- **Network limits:** egress and ingress bandwidth and packet-rate limits, plus TCP and UDP connection caps.

## 5. Networking

- **Publishing ports:** `-p HOST:GUEST`, `-p BIND_ADDR:HOST:GUEST`, ranges, `/udp`, and `[::1]:...` for IPv6. Published ports **bind to `127.0.0.1` by default**. Use `-p 0.0.0.0:...` only to deliberately expose a port beyond localhost.
- **Changing ports later:** ports are a restart-or-next-start change through modify. The docs list this in the tuning table and the SDKs support it, but **`msb modify --help` (0.7.8) has no `--port` flag**. Plan to set ports at create time.
- **Guest binding:** official guides make guest services listen on `0.0.0.0` (e.g. jupyter `--ip=0.0.0.0`, redis `--bind 0.0.0.0`). Assume the Orca web UI must bind to 0.0.0.0 inside the guest (see Unknowns).
- **Not affected by multi-tenant restrictions:** published ports are disabled in the `multi-tenant` deployment profile. The default profile is `single-tenant`, so they work by default.
- **Egress default:** the guest can reach the **public internet only**. Private networks, loopback, link-local and cloud metadata are denied.
- **Egress controls:**
  - `--no-net`
  - `--net public,private,host` (composable profiles)
  - `--net-rule "<allow|deny>[:egress|ingress]@<target>[:proto[:ports]]"`. Targets can be IPs, CIDRs, domains, `*.suffix`, groups, or `dns`. The first matching rule wins.
  - `--net-default[-egress|-ingress]`
  - Hostname allows are **strict** by default: HTTPS allowed by hostname needs TLS interception, or `--net-strict=false`.
  - Other options: DNS rebinding protection, custom nameservers, an upstream `--proxy`, and `--trust-host-cas`.
  - (docs:/networking/overview)
- **Reaching the host:** `host.microsandbox.internal`, which needs the `host` profile, e.g. `--net public,host`.
- **Host sockets:** `--vsock HOST_PATH:PORT` exposes a host Unix socket to the guest without opening TCP (docs:/networking/host-sockets).

## 6. Secrets

- **Mechanism:** placeholder substitution at the **host-side egress proxy**. It is not plain environment injection.
- **CLI:** `--secret 'ENV[:OPTIONS]@HOST[,HOST...]'`.
  - The real value is read from the **host** environment variable `ENV` at start time. msb stores only a reference to that variable. Inline `ENV=VALUE` is rejected.
  - Inside the guest, `$ENV` holds a literal placeholder string, by default `$MSB_<ENV>`.
  - On egress to an allowed host, the proxy decrypts the intercepted TLS and checks that:
    - the SNI matches the secret's allowed hosts
    - the destination IP was resolved for that host through msb's DNS ("DNS pin")
    - the `Host` / `:authority` header matches the SNI
  - Only then does it swap in the real value.
  - (docs:/sandboxes/secrets, docs:/security/secrets)
- **Per-host scoping: yes.** It is the allow list.
  - **Request locations:** headers (including Basic auth) are on by default and can be limited with `headers=authorization`. Query and body are opt-in (`:query`, `:body`). Body substitution is HTTP/1 only, up to 16 MiB; HTTP/2 bodies are blocked.
  - **Violation actions:** `block`, `block-and-log` (the default), and `block-and-terminate`. A request to a non-allowed host carrying the placeholder is blocked.
  - **Passthrough:** `passthrough=<host>` lets a named host receive the placeholder unchanged.
- **TLS trust:** secrets require TLS interception, which is enabled automatically. msb adds its CA (`~/.microsandbox/tls/ca.{crt,key}`) to the guest system trust store. Applications that keep their own trust store, such as Node, Python certifi, or **containers inside Docker**, need extra setup; the `scripts/msb-trust/msb-trust.sh` helper covers common cases (docs:/networking/tls).
- **Can guest code read the raw value? No**, for values passed as secrets. The guest only ever holds the placeholder. Caveats from the docs:
  - An allowed endpoint that echoes the credential back exposes it.
  - Placeholders cannot be used for credentials that are signed inside the guest.
  - A value passed with plain `-e` is fully visible to the guest.
  - Raw values passed through an SDK (rather than an environment reference) are persisted in the host-side configuration.
- **OAuth and token refresh (analysis; not documented):**
  - The feature only models **static credentials sourced from host environment variables**. It does not substitute values inside files and does not rewrite responses.
  - An OAuth refresh therefore cannot be protected:
    - The refresh token would need body substitution, which is opt-in and HTTP/1 only.
    - The **new access and refresh tokens come back in the response, in plaintext, to the guest**.
    - The guest-side credentials file would then hold the real tokens.
  - The project's own guides for **Claude Code** (docs:/how-to/agents/claude-code) and **Codex** (docs:/how-to/agents/codex) do **not** use secrets. They log in *inside* the VM (browser URL or device code opened on the host), and the credentials live on the sandbox root disk (`/root/.codex`, Claude config).
  - Practical options for orcasm:
    - **`gh`** works with a secret: `GH_TOKEN@api.github.com,github.com` with header substitution, since gh sends `Authorization: token $GH_TOKEN`. Git-over-HTTPS to github.com would also need Basic-auth substitution (enabled by default with headers).
    - **Anthropic or OpenAI API keys** work as secrets: `ANTHROPIC_API_KEY@api.anthropic.com`, `OPENAI_API_KEY@api.openai.com`.
    - **Subscription OAuth** (a Claude Max credentials file, a ChatGPT `auth.json`) must be treated as **guest-readable**. Either log in inside the VM, or copy or mount the host credential files and accept that the guest can read them.

## 7. Volumes

- **Bind mounts:** `--mount-dir SRC:DEST[:ro|rw,noexec,nosuid,nodev,quota=,uid=,gid=,stat-virt=,host-perms=]` (or `-v`), plus `--mount-file`.
  - Directory mounts use **virtiofs**.
  - Relative host paths are saved as absolute paths at creation.
  - Read-only is available with `ro`/`readonly`.
  - Writable binds get a default quota; the user's `oc-dev` shows `quota=102400MiB`.
- **Named volumes:** `msb volume create NAME [--kind dir|disk --size 20G]`, stored in `~/.microsandbox/volumes/<name>/`.
  - `kind=dir` mounts through virtiofs and can be shared between sandboxes.
  - `kind=disk` is a raw ext4 image on virtio-blk, with one writer at a time.
  - `--mount-named name:/path[:kind=disk,size=20G]` creates the volume if needed. Named volumes survive `rm`.
- **Owned volumes:** `--mount-owned /path[:kind=disk,size=10G]` is private to one sandbox, survives stop, and is deleted on `rm`.
- **Other mount types:** `--mount-disk` attaches a host qcow2/raw/vmdk, and `--tmpfs` mounts a RAM filesystem.
- **Performance:** the docs say mounts are "significantly faster" than the file API. The file API and `msb cp` transfer one file at a time. No virtiofs-versus-block benchmarks are published. The flat root is described as better for "filesystem-heavy work" (docs:/sandboxes/volumes, docs:/sandboxes/optimization).
- **Restriction:** the `--security restricted` profile is **incompatible with Docker-in-Docker and sudo**.

## 8. Docker inside the VM

- **Officially supported** (docs:/how-to/docker/docker-in-sandbox). `dockerd` runs inside the guest (example image: `docker:dind`).
- **Required:** do not put `/var/lib/docker` on the default OverlayFS root. Use one of:
  - `--root-disk flat:10G`
  - `--mount-owned /var/lib/docker:kind=disk,size=10G`
  - `--mount-named docker-data:/var/lib/docker:kind=disk,size=20G`, which keeps Docker data independent of the sandbox
- **Past bug:** in the 2026-06-26 changelog, disk-kind named volumes failed with `EINVAL` for overlayfs/Docker. It is fixed.
- **Prior art:** the user's `oc-dev` uses ubuntu-systemd, `--init auto`, and an owned ext4 disk at `/var/lib/docker`. This is a working pattern for systemd-managed `dockerd` plus Compose.
- **cgroups:** not documented specifically. It works in the official guide and in `oc-dev`; the cgroup version and delegation details are unverified.
- **Nested virtualization (KVM inside the sandbox):** not documented and assumed unavailable. Docker containers do not need it.
- **Docker Compose:** not mentioned. It is just a client plugin, so it should work wherever dockerd works.

## 9. SSH and exec

- **Built-in SSH:** there is no sshd in the guest. msb speaks SSH on the host side and forwards shells, exec, SFTP and `-L`/`-D` forwarding into the guest (docs:/sandboxes/ssh).
  - `msb ssh NAME [-- cmd]` opens a native session with no keys needed.
  - `msb ssh authorize --file ~/.ssh/id_ed25519.pub` adds a key to `<MSB_HOME>/ssh/authorized_keys`. This is global, not per sandbox.
  - `msb ssh serve NAME [--host 127.0.0.1 --port 2222]` runs a TCP listener, loopback by default. The SSH user name selects the guest user (root or any other; see §11).
  - **Best fit for orcasm:** an `ssh_config` `Host` with `ProxyCommand msb ssh serve NAME --stdio`. The tested recipe, logging in as `dev`, is in §11. It works for VS Code Remote-SSH, scp, sftp and rsync.
  - Limitations:
    - No reverse forwarding (`-R`) and no stream-local forwarding.
    - Sessions disconnect after 10 minutes idle by default. Change this with `"ssh": {"inactivity_timeout_secs": 0}` in `~/.microsandbox/config.json` or with `--no-inactivity-timeout`.
- **Alternative:** run a real sshd in the guest and publish it with `-p 127.0.0.1:2222:22`. This is not documented but should work like any other published port.
- **Exec and shell:**
  - `msb exec [-t] [-d] [-u user] [-w dir] [-e K=V] NAME -- cmd`. With no command, it attaches the default shell.
  - Detached jobs: `msb jobs`, `attach`, `signal`, `kill`, `eof`.
  - Exec uses a virtio-console agent channel, not the network.

## 10. SDKs and maturity

- **SDKs:** TypeScript (`npm i microsandbox`), Rust (`cargo add microsandbox`), Python (`pip install microsandbox`), Go (`github.com/superradcompany/microsandbox/sdk/go`) and Ruby (partial; for example, no mount config). They expose the same features as the CLI, plus low-level "agent client" protocol references. An MCP server lives in the `mcp/` directory. Setting `MSB_BACKEND=cloud` with `MSB_API_KEY` switches to the hosted cloud.
- **Maturity:**
  - The README says: "Microsandbox is still **beta software**. Expect breaking changes, missing features, and rough edges."
  - Releases ship very often: v0.7.3 to v0.7.8 came out between 2026-09-24 and 2026-10-09, and there were weekly changelogs before that.
  - v0.6 to v0.7 had a migration guide: secret-policy semantics changed, and snapshot members became immutable.
  - Early v0.7 releases had compatibility gaps; docs say to use ≥ 0.7.3.
  - All SDK and CLI processes sharing `MSB_HOME` must be upgraded together.
  - Public CLI and SDK compatibility is explicitly outside the internal `COMPATIBILITY.md` contract.
  - About 8.7k GitHub stars.
- **Advice:** pin the `msb` version in orcasm and validate it with `msb --version`.

## 11. SSH as the non-root `dev` user (spike #7, tested)

Tested 2026-10-16 on this host with msb 0.7.8, `ubuntu-systemd:24.04`, `--init auto`, and VS Code 1.141.0 with Remote-SSH 0.128.0.

**Answer: yes, msb SSH logs in as any existing guest user.** The SSH user name is the guest user. `ssh -o User=dev` runs as `uid=dev` with that user's supplementary groups (e.g. `docker`) and `HOME=/home/dev`. No switch on login, `su` wrapper or guest sshd is needed. An unknown user fails with `exec session error: guest user not found: <name>`.

**Verified as `dev`:**
- Commands, interactive PTY shells, sftp, scp and rsync (rsync must be installed in the guest). Files are created `dev:dev`.
- `-L` and `-D` forwarding to a `dev`-owned listener on guest `127.0.0.1`.
- **VS Code Remote-SSH, end to end.** VS Code runs `ssh -T -D <port> -F <cfg> <host> bash`, downloads its CLI and server into `/home/dev/.vscode-server`, and connects through the `-D` SOCKS tunnel. Every server process, including the extension host, ptyHost, file watcher and terminals, runs as uid `dev` with groups `dev,docker`. So editor saves and forwarded ports run as `dev` too. Nothing was written to `/root`.
- **Public-key authentication is enforced** in `--stdio` mode. A key missing from msb's `authorized_keys` gets `Permission denied`.

**What msb does not take from `/etc/passwd`:**
- **Shell:** msb ignores the passwd shell. Commands and interactive sessions use the sandbox's `--shell`, which defaults to `/bin/sh`.
- **Working directory:** sessions start in the sandbox's `--workdir` (default `/`), not in `$HOME`.
- **Environment:** `USER`, `LOGNAME` and `SHELL` are not set.
- Both `--shell` and `--workdir` apply to `msb exec` and to SSH, for all users. `--workdir` can be changed later with `msb modify -w`. `--shell` is create-time only.

**Host keys:**
- Each sandbox has its own host key in `~/.microsandbox/sandboxes/<name>/ssh/host_ed25519` (an msb-internal path). The key survives stop/start and is regenerated when the sandbox is removed and recreated under the same name.
- `ssh-keygen -y -f <that file>` prints the public key, so orcasm can write `known_hosts` itself instead of relying on trust-on-first-use.

**Authorization:**
- **Scope:** `msb ssh authorize` is idempotent ("Already authorized"). It appends to the single, global `~/.microsandbox/ssh/authorized_keys`. A key in that file can log in to **every** sandbox of this `MSB_HOME`, as **any** user, root included. Logging in as `dev` is a convenience for file ownership, not a security boundary.
- **Revocation:** there is no command to remove a key. Edit the file directly.

**Inactivity timeout:**
- With `--inactivity-timeout 15s` and no keepalive, an idle session (`sleep 30`) hung instead of returning; it was killed by `timeout` after 60 s.
- With `ServerAliveInterval 5`, the same session survived. SSH keepalives count as traffic.
- A VS Code session idle for longer than the 10-minute default would otherwise risk hanging, so the recipe below disables the timeout and sets keepalives anyway.

### Recipe for #12 (provisioning)

1. Create the sandbox with `--shell /bin/bash` and `--workdir <Main path>` (or set the workdir later with `msb modify -w`). This gives VS Code terminals and `ssh` a bash shell in Main.
2. As root, run `useradd -m -s /bin/bash dev` and add `dev` to the groups it needs, e.g. `usermod -aG docker dev`. On `ubuntu-systemd:24.04`, uid 1000 is already taken by `ubuntu`, so `dev` gets 1001 unless orcasm picks the uid itself.
3. Make the Main checkout and everything VS Code edits owned by `dev`.
4. Install the packages that VS Code's server install and rsync need: `curl` or `wget`, `tar`, `ca-certificates`, `rsync`. The base image ships none of `curl`, `wget` or `python3`.

### Recipe for #14 (ssh config)

1. **Key:**
   - Generate an orcasm-owned key once, e.g. `~/.orcasm/ssh/id_ed25519`.
   - Run `msb ssh authorize --file <key>.pub`. It is safe to repeat.
   - Never modify the user's other keys.
2. **Known hosts:** write `~/.orcasm/ssh/known_hosts` from `ssh-keygen -y -f ~/.microsandbox/sandboxes/<name>/ssh/host_ed25519`. Prefix each line with the `Host` alias. Refresh the entry when a sandbox is recreated.
3. **Config file:** write orcasm's own file, e.g. `~/.orcasm/ssh/config`, with one block per sandbox:
   ```
   Host orcasm-<name>
     User dev
     ProxyCommand msb ssh serve <name> --stdio --no-inactivity-timeout
     IdentityFile ~/.orcasm/ssh/id_ed25519
     IdentitiesOnly yes
     UserKnownHostsFile ~/.orcasm/ssh/known_hosts
     StrictHostKeyChecking yes
     ServerAliveInterval 30
   ```
4. **Hook into the user's config:** add one `Include ~/.orcasm/ssh/config` line to `~/.ssh/config`, so VS Code Remote-SSH lists the hosts. Alternatively, point VS Code's `remote.SSH.configFile` setting at orcasm's file.
5. **Root access:** keep root out of the `Host` entry. When root is needed, use `msb exec` (default user root) or `ssh -o User=root`.

---

## Unknown / unverified

- **Guest binding for published ports:** does a published port reach a guest service bound only to guest `127.0.0.1`? Docs examples always bind `0.0.0.0`; assume this is required.
- **Changing ports from the CLI:** `msb modify` (0.7.8) has no `--port` flag, although the docs say ports are modifiable with a restart. This is probably SDK/config-only, or a docs/CLI mismatch.
- **Default root disk size** for new local OCI sandboxes in 0.7.8. 4 GiB was the legacy default.
- **Contradiction on flat roots:** `docs/sandboxes/optimization.mdx` says flat roots don't support patches or snapshots, while `docs/cli/sandbox-commands.mdx` says they do.
- **Exact label matching:** whether a bare `--label orcasm` selector does exact empty-value matching or key-exists matching. Use `orcasm=1` to sidestep the question.
- **Docker inside the VM:** cgroup v1 or v2, cgroup delegation, and whether nested containers trust the msb TLS-interception CA. Interception only applies when enabled, e.g. by a secret; containers ship their own CA bundles.
- **Interception scope:** whether enabling TLS interception (implied by any secret) breaks certificate pinning or HTTP/2 for other hosts. `--tls-bypass` exists to exclude domains.
- **OAuth:** any first-party support for OAuth or refreshing credentials. None was found in docs or source; the conclusion in §6 is inference.
- **Nested KVM inside a sandbox:** not documented.
- **Behaviour with the host offline or asleep**, and whether detached sandboxes survive a host reboot. They are child processes, so they presumably stop and must be `msb start`ed again; there is no autostart mechanism documented.
