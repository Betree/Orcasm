package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// OrcaUnit is the systemd unit running `orca-ide serve` in the guest.
const OrcaUnit = "orca"

const webClientPrefix = "Web client URL: "

// orcaRunLog prints the log of the Orca service's current run only: each
// serve start mints a new pairing device and URL (docs/research/orca.md), so
// earlier runs' lines are skipped. It fails while systemd or the unit is not
// up yet.
const orcaRunLog = `id=$(systemctl show -p InvocationID --value ` + OrcaUnit + `) && [ -n "$id" ] && ` +
	`journalctl --no-pager -o cat _SYSTEMD_INVOCATION_ID="$id"`

// URL returns the link that pairs a browser with the Sandbox's Orca web UI.
// It is read from Orca's service log each time and never stored (it holds a
// device token). Right after a start Orca takes a few seconds to print it,
// so URL waits until ctx is done.
func (m Manager) URL(ctx context.Context, name string) (string, error) {
	s, err := m.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if !s.Running() {
		return "", fmt.Errorf("Sandbox %q is %s; run `orcasm start %s` first", name, strings.ToLower(s.Status), name)
	}
	poll := m.Poll
	if poll == 0 {
		poll = time.Second
	}
	for {
		log, err := m.MSB.Exec(ctx, name, "", "sh", "-c", orcaRunLog)
		if err == nil {
			if url, ok := lastWebClientURL(log); ok {
				return url, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("Orca in %q has not printed its web client URL; check its log with `msb exec %s -- journalctl -u %s`",
				name, name, OrcaUnit)
		case <-time.After(poll):
		}
	}
}

func lastWebClientURL(log string) (string, bool) {
	lines := strings.Split(log, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if url, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), webClientPrefix); ok && url != "" {
			return url, true
		}
	}
	return "", false
}
