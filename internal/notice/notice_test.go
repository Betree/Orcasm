package notice_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/notice"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func daily(t *testing.T) (notice.Daily, *clock) {
	c := &clock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	return notice.Daily{Stamp: filepath.Join(t.TempDir(), "orcasm", "upgrade-notice"), Now: c.Now}, c
}

func TestTheFirstCheckIsDue(t *testing.T) {
	d, _ := daily(t)
	if !d.Due() {
		t.Error("never checked, yet not due")
	}
}

func TestACheckIsDueAtMostOncePerDay(t *testing.T) {
	d, c := daily(t)
	if err := d.Mark(); err != nil {
		t.Fatal(err)
	}

	c.now = c.now.Add(23 * time.Hour)
	if d.Due() {
		t.Error("due again within a day")
	}
	c.now = c.now.Add(2 * time.Hour)
	if !d.Due() {
		t.Error("not due a day later")
	}
}
