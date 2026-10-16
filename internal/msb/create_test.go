package msb_test

import (
	"reflect"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
)

func TestCreateOptionsArgs(t *testing.T) {
	opts := msb.CreateOptions{
		Image:       "ghcr.io/superradcompany/ubuntu-systemd:24.04",
		Name:        "demo",
		Labels:      map[string]string{"orcasm": "1", "orcasm.project": "/home/u/demo"},
		CPUs:        4,
		MemoryGiB:   8,
		RootDiskGiB: 32,
		OwnedDisks:  []msb.OwnedDisk{{Path: "/var/lib/docker", SizeGiB: 100}},
		Ports:       []msb.Port{{HostAddr: "127.0.0.1", Host: 6769, Guest: 6768}},
		Shell:       "/bin/bash",
		Workdir:     "/home/dev/demo",
		Mkdirs:      []string{"/home/dev/demo"},
	}
	want := []string{
		"create", "ghcr.io/superradcompany/ubuntu-systemd:24.04", "--name", "demo", "--quiet",
		"--init", "auto",
		"--cpus", "4", "--memory", "8G", "--root-disk", "32G",
		"--label", "orcasm=1", "--label", "orcasm.project=/home/u/demo",
		"--mount-owned", "/var/lib/docker:kind=disk,size=100G",
		"--port", "127.0.0.1:6769:6768",
		"--shell", "/bin/bash",
		"--mkdir", "/home/dev/demo",
		"--workdir", "/home/dev/demo",
	}
	if got := opts.Args(); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
