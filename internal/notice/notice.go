// Package notice throttles orcasm's daily one-line notice that a Sandbox
// upgrade is pending (ADR 0006): the check reads the network and every
// Sandbox's labels, so it runs at most once a day, recorded in a stamp
// file's modification time.
package notice

import (
	"os"
	"path/filepath"
	"time"
)

// Interval is how long a check lasts: the next is due once it has passed.
const Interval = 24 * time.Hour

// Daily is a once-a-day check.
type Daily struct {
	// Stamp is the file whose modification time is the last check
	// (normally <user cache dir>/orcasm/upgrade-notice).
	Stamp string
	// Now is the clock (default time.Now).
	Now func() time.Time
}

func (d Daily) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// Due reports whether a day has passed since the last check, or none was
// recorded.
func (d Daily) Due() bool {
	fi, err := os.Stat(d.Stamp)
	if err != nil {
		return true
	}
	return d.now().Sub(fi.ModTime()) >= Interval
}

// Mark records a check now. Mark before checking: a check that fails or
// is interrupted then waits a day too, instead of slowing every command.
func (d Daily) Mark() error {
	if err := os.MkdirAll(filepath.Dir(d.Stamp), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(d.Stamp, nil, 0o600); err != nil {
		return err
	}
	now := d.now()
	return os.Chtimes(d.Stamp, now, now)
}
