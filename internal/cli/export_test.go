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
	create = func(ctx context.Context, _ BuildInfo, cfg wizard.Config, sv sandbox.SecretValue, out io.Writer) error {
		return f(ctx, cfg, sv, out)
	}
	return func() { create = old }
}

// WantsUpgradeNotice reports whether `orcasm <args>` would print the daily
// upgrade notice, with stdout a terminal or not and the environment env.
func WantsUpgradeNotice(args []string, tty bool, env map[string]string) bool {
	cmd, _, err := NewRootCommand(BuildInfo{Version: "dev"}).Find(args)
	if err != nil {
		panic(err)
	}
	return noticeWanted(cmd, tty, func(k string) string { return env[k] })
}
