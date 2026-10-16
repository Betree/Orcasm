package sandbox_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

const orcaStartLog = `[orca] starting
Web client URL: http://127.0.0.1:6768/web-index.html#pairing=orca%3A%2F%2Fpair%3Fcode%3Dfirst
Pairing URL: orca://pair?code=first
`

func TestURLIsTheCurrentOrcaRunsWebClientURL(t *testing.T) {
	f := newFakeMSB()
	sb := f.add("app", msb.Running, "orcasm=1")
	sb.orcaLog = []string{orcaStartLog + "some noise\n" +
		"Web client URL: http://127.0.0.1:6768/web-index.html#pairing=orca%3A%2F%2Fpair%3Fcode%3Dsecond\n"}

	got, err := sandbox.Manager{MSB: f}.URL(context.Background(), "app")
	if err != nil {
		t.Fatal(err)
	}

	if want := "http://127.0.0.1:6768/web-index.html#pairing=orca%3A%2F%2Fpair%3Fcode%3Dsecond"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestURLWaitsForOrcaToPrintIt(t *testing.T) {
	f := newFakeMSB()
	sb := f.add("app", msb.Running, "orcasm=1")
	sb.orcaLog = []string{"", "[orca] starting\n", orcaStartLog}

	got, err := sandbox.Manager{MSB: f, Poll: time.Millisecond}.URL(context.Background(), "app")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(got, "code%3Dfirst") {
		t.Errorf("URL = %q, want the first run's URL", got)
	}
}

func TestURLGivesUpWhenOrcaNeverPrintsIt(t *testing.T) {
	f := newFakeMSB()
	sb := f.add("app", msb.Running, "orcasm=1")
	sb.orcaLog = []string{"[orca] starting\n"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := sandbox.Manager{MSB: f, Poll: time.Millisecond}.URL(ctx, "app")

	if err == nil || !strings.Contains(err.Error(), "journalctl -u orca") {
		t.Errorf("err = %v, want a pointer at Orca's service log", err)
	}
}

func TestURLOfAStoppedSandboxSaysToStartIt(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Stopped, "orcasm=1")

	_, err := sandbox.Manager{MSB: f}.URL(context.Background(), "app")

	if err == nil || !strings.Contains(err.Error(), "orcasm start app") {
		t.Errorf("err = %v, want a hint to start it", err)
	}
}
