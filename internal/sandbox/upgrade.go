package sandbox

import (
	"context"
	"fmt"
	"io"
)

// Upgrade brings the named Sandbox up to this orcasm in place (ADR 0006):
// first its pending migrations, then (#23) version reconciliation. A
// stopped Sandbox is started first, since both need systemd and Orca's
// service; it is left running.
func (m Manager) Upgrade(ctx context.Context, name string, out io.Writer) error {
	s, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if !s.Running() {
		fmt.Fprintf(out, "==> Starting %s\n", name)
		if err := m.Start(ctx, name); err != nil {
			return err
		}
	}
	return m.Migrate(ctx, name, out)
}
