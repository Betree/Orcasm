// Package wizard collects every input needed to create a Sandbox. The Wizard
// only collects: it returns a Config that later steps (tokens, provisioning,
// copying the Project) consume. It never creates anything besides saving its
// answers as defaults for next time.
package wizard

// Config is everything the Wizard collected for one Sandbox.
type Config struct {
	// ProjectPath is the absolute path of the Project on the host.
	ProjectPath string `toml:"project_path,omitempty"`
	// Name is the Sandbox name; unique among existing Sandboxes.
	Name      string    `toml:"name,omitempty"`
	Resources Resources `toml:"resources,omitempty"`
	// HostPort is the host port Orca's web UI is forwarded to.
	HostPort int `toml:"host_port,omitempty"`
	// Harnesses are the agent harnesses to install in the Sandbox
	// (subset of SupportedHarnesses).
	Harnesses []string `toml:"harnesses,omitempty"`
	Project   Project  `toml:"project,omitempty"`
	// Secrets are the host tokens the user approved sharing with the Sandbox,
	// as specs: the values stay on the host.
	Secrets []Secret `toml:"secrets,omitempty"`
	// Git is the identity commits are made with in the Sandbox; empty means
	// none was shared.
	Git GitIdentity `toml:"git,omitempty"`
	// LinkSSHConfig is the user's consent to add one Include line to
	// ~/.ssh/config, so VS Code Remote-SSH lists the Sandbox. Asked only
	// while that line is missing, and never remembered.
	LinkSSHConfig bool `toml:"link_ssh_config,omitempty"`
}

// GitIdentity is a git user.name and user.email.
type GitIdentity struct {
	Name  string `toml:"name,omitempty"`
	Email string `toml:"email,omitempty"`
}

// Resources sizes the Sandbox.
type Resources struct {
	CPUs          int `toml:"cpus,omitempty"`
	MemoryGiB     int `toml:"memory_gib,omitempty"`
	RootDiskGiB   int `toml:"root_disk_gib,omitempty"`
	DockerDiskGiB int `toml:"docker_disk_gib,omitempty"` // sparse disk at /var/lib/docker
}

// Project holds the project-level answers, saved to the committable
// orcasm.toml at the root of the Project.
type Project struct {
	// ComposeFiles are paths relative to the Project making up the Stack.
	ComposeFiles []string `toml:"compose_files,omitempty"`
	// Workbench is the Compose service used as Workbench; empty means the
	// generic fallback Workbench.
	Workbench string `toml:"workbench,omitempty"`
	// WebService is the Compose service serving the Project's web app, if any,
	// and WebPort the container port it listens on.
	WebService string `toml:"web_service,omitempty"`
	WebPort    int    `toml:"web_port,omitempty"`
	// BringFiles are gitignored paths (relative to the Project) copied
	// into the Sandbox along with the tracked files, e.g. `.env`.
	BringFiles []string `toml:"bring_files,omitempty"`
}

// SupportedHarnesses are the agent harnesses orcasm has adapters for.
var SupportedHarnesses = []string{"claude", "codex", "opencode"}
