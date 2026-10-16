# Threat model: the host is the hard boundary

Orcasm guarantees that agents cannot reach the host kernel or filesystem: the Project is copied into the Sandbox (no host mount in v1), and the Sandbox is a microsandbox VM. Static tokens (`gh`, API keys) are injected through microsandbox secrets, so agents can use them but only ever see a placeholder. Subscription logins (Claude Code, Codex OAuth) cannot be protected that way — microsandbox only substitutes static values in outgoing requests, and a token refresh hands fresh tokens to the guest — so they live on the Sandbox disk and are readable by agents. Network egress is unrestricted in v1.

## Considered: injecting the host's live OAuth token at egress

Rejected for v1. msb reads a secret's value once, from a host env var, when the sandbox starts; access tokens expire within hours, so the proxy would go stale. Making it work would mean forging harness credential files with fake expiries, plus a host-side refresher whose token rotation logs the host's own harness out. That's fragile against harness updates and dubious under subscription terms. The Wizard therefore does no subscription sign-in; users sign in the first time they open an agent.

## Consequences

- A compromised agent can exfiltrate subscription credentials and anything in the Sandbox; users must treat the Sandbox as an untrusted machine that holds those logins.
- Mounting the host folder is deliberately absent: agent-written `.git/hooks`, `.envrc`, editor task files would execute on the host.
