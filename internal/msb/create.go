package msb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// CreateOptions describes a sandbox for `msb create`. The sandbox boots
// detached with `--init auto`, so images with systemd run it as PID 1.
type CreateOptions struct {
	Image string
	Name  string
	// Labels are attached as KEY=VALUE.
	Labels      map[string]string
	CPUs        int
	MemoryGiB   int
	RootDiskGiB int
	// OwnedDisks are ext4 disks private to the sandbox, deleted with it.
	OwnedDisks []OwnedDisk
	Ports      []Port
	// Shell and Workdir apply to msb exec and msb SSH sessions, for every
	// user. Shell is create-time only; Workdir must exist at boot, hence
	// Mkdirs.
	Shell   string
	Workdir string
	// Mkdirs are created in the guest root filesystem before boot.
	Mkdirs []string
}

// OwnedDisk is an `--mount-owned PATH:kind=disk,size=NG` volume.
type OwnedDisk struct {
	Path    string
	SizeGiB int
}

// Port publishes guest port Guest on HostAddr:Host.
type Port struct {
	HostAddr string
	Host     int
	Guest    int
}

// Args is the `msb create` argv (without the msb binary).
func (o CreateOptions) Args() []string {
	args := []string{"create", o.Image, "--name", o.Name, "--quiet", "--init", "auto"}
	if o.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(o.CPUs))
	}
	if o.MemoryGiB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dG", o.MemoryGiB))
	}
	if o.RootDiskGiB > 0 {
		args = append(args, "--root-disk", fmt.Sprintf("%dG", o.RootDiskGiB))
	}
	keys := make([]string, 0, len(o.Labels))
	for k := range o.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--label", k+"="+o.Labels[k])
	}
	for _, d := range o.OwnedDisks {
		args = append(args, "--mount-owned", fmt.Sprintf("%s:kind=disk,size=%dG", d.Path, d.SizeGiB))
	}
	for _, p := range o.Ports {
		args = append(args, "--port", fmt.Sprintf("%s:%d:%d", p.HostAddr, p.Host, p.Guest))
	}
	if o.Shell != "" {
		args = append(args, "--shell", o.Shell)
	}
	for _, d := range o.Mkdirs {
		args = append(args, "--mkdir", d)
	}
	if o.Workdir != "" {
		args = append(args, "--workdir", o.Workdir)
	}
	return args
}

// Create creates and boots the sandbox (`msb create`).
func (c CLI) Create(ctx context.Context, opts CreateOptions) error {
	return c.stream(ctx, io.Discard, opts.Args()...)
}

// ExecTo runs argv in sandbox name as user ("" means root) with no stdin,
// streaming its stdout to out. A non-zero exit becomes an error carrying
// stderr.
func (c CLI) ExecTo(ctx context.Context, name, user string, out io.Writer, argv ...string) error {
	args := []string{"exec", "--quiet", "--no-stdin"}
	if user != "" {
		args = append(args, "--user", user)
	}
	return c.stream(ctx, out, append(append(args, name, "--"), argv...)...)
}

func (c CLI) stream(ctx context.Context, out io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "msb", args...)
	if c.Home != "" {
		cmd.Env = append(os.Environ(), "MSB_HOME="+c.Home)
	}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("msb %s: %w: %s", args[0], err, lastLines(stderr.String(), 20))
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
