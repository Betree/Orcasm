package cli

import (
	"context"
	"io"

	"github.com/Betree/Orcasm/internal/wizard"
)

// SetProvision replaces how `orcasm create` provisions the Sandbox, so tests
// never boot a VM. It returns a func restoring the real one.
func SetProvision(f func(context.Context, wizard.Config, io.Writer) error) (restore func()) {
	old := provision
	provision = f
	return func() { provision = old }
}
