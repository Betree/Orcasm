// Package wizard collects every input needed to create a Sandbox. The Wizard
// only collects: it returns a Config that later steps (tokens, provisioning,
// copying the Project) consume. It never creates anything besides saving its
// answers as defaults for next time.
package wizard

// Config is everything the Wizard collected for one Sandbox.
type Config struct {
	// Folder is the absolute path of the Project on the host.
	Folder string `toml:"folder,omitempty"`
	// Name is the Sandbox name; unique among existing Sandboxes.
	Name      string    `toml:"name,omitempty"`
	Resources Resources `toml:"resources,omitempty"`
	// HostPort is the host port Orca's web UI is forwarded to.
	HostPort int `toml:"host_port,omitempty"`
	// Harnesses are the agent harnesses to install in the Sandbox
	// (subset of SupportedHarnesses).
	Harnesses []string `toml:"harnesses,omitempty"`
	Project   Project  `toml:"project,omitempty"`
}

// Resources sizes the Sandbox VM.
type Resources struct {
	CPUs          int `toml:"cpus,omitempty"`
	MemoryGiB     int `toml:"memory_gib,omitempty"`
	RootDiskGiB   int `toml:"root_disk_gib,omitempty"`
	DockerDiskGiB int `toml:"docker_disk_gib,omitempty"` // sparse disk at /var/lib/docker
}

// Project holds the project-level answers, saved to the committable
// orcasm.toml at the root of the Project.
type Project struct {
	// ComposeFiles are paths relative to the Project folder making up the Stack.
	ComposeFiles []string `toml:"compose_files,omitempty"`
	// Workbench is the Compose service used as Workbench; empty means the
	// generic fallback Workbench.
	Workbench string `toml:"workbench,omitempty"`
	// WebService is the Compose service serving the Project's web app, if any,
	// and WebPort the container port it listens on.
	WebService string `toml:"web_service,omitempty"`
	WebPort    int    `toml:"web_port,omitempty"`
	// BringFiles are gitignored paths (relative to the Project folder) copied
	// into the Sandbox along with the tracked files, e.g. `.env`.
	BringFiles []string `toml:"bring_files,omitempty"`
}

// SupportedHarnesses are the agent harnesses orcasm has adapters for.
var SupportedHarnesses = []string{"claude", "codex", "opencode"}
