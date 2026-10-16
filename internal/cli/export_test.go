package cli

import (
	"context"
	"io"

	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
)

// SetCreate replaces how `orcasm create` creates the Sandbox, so tests
// never boot a VM. It returns a func restoring the real one.
func SetCreate(f func(context.Context, wizard.Config, sandbox.SecretValue, io.Writer) error) (restore func()) {
	old := create
	create = f
	return func() { create = old }
}
