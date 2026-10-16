package msb_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
)

func TestCreateOptionsArgs(t *testing.T) {
	opts := msb.CreateOptions{
		Image:       "ghcr.io/superradcompany/ubuntu-systemd:24.04",
		Name:        "demo",
		Labels:      []string{"orcasm=1", "orcasm.folder=/home/u/demo"},
		CPUs:        4,
		MemoryGiB:   8,
		RootDiskGiB: 32,
		OwnedDisks:  []msb.OwnedDisk{{Path: "/var/lib/docker", SizeGiB: 100}},
		Ports:       []msb.Port{{HostAddr: "127.0.0.1", Host: 6769, Guest: 6768}},
		Shell:       "/bin/bash",
		Workdir:     "/home/dev/demo",
		Mkdirs:      []string{"/home/dev/demo"},
		Secrets: []msb.Secret{
			{Name: "GH_TOKEN", Hosts: []string{"github.com", "api.github.com"}, Value: "ghp_real"},
		},
	}
	want := []string{
		"create", "ghcr.io/superradcompany/ubuntu-systemd:24.04", "--name", "demo", "--quiet",
		"--init", "auto",
		"--cpus", "4", "--memory", "8G", "--root-disk", "32G",
		"--label", "orcasm=1", "--label", "orcasm.folder=/home/u/demo",
		"--mount-owned", "/var/lib/docker:kind=disk,size=100G",
		"--port", "127.0.0.1:6769:6768",
		"--shell", "/bin/bash",
		"--mkdir", "/home/dev/demo",
		"--workdir", "/home/dev/demo",
		"--secret", "GH_TOKEN@github.com,api.github.com",
	}
	if got := opts.Args(); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	for _, a := range opts.Args() {
		if strings.Contains(a, "ghp_real") {
			t.Errorf("secret value leaked into argv: %q", a)
		}
	}
}
