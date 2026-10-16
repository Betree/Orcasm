package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() { bareRun = runBare }

// runBare is `orcasm` with no subcommand: list the Sandboxes, then get the
// current folder's Sandbox running and print its URL, offering to start it,
// or to create one when the folder has none.
func runBare(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	in := bufio.NewReader(cmd.InOrStdin())
	m, err := newManager()
	if err != nil {
		return err
	}
	all, err := m.List(ctx)
	if err != nil {
		return err
	}
	printSandboxes(out, all)
	fmt.Fprintln(out)

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	here, err := m.InFolder(ctx, cwd)
	if err != nil {
		return err
	}
	switch {
	case len(here) == 0:
		if !confirm(in, out, fmt.Sprintf("No Sandbox for %s. Create one?", cwd)) {
			return nil
		}
		return runCreate(cmd)
	case len(here) > 1:
		fmt.Fprintf(out, "%s has several Sandboxes; run `orcasm start <name>`.\n", cwd)
		return nil
	}
	s := here[0]
	if !s.Running() {
		if !confirm(in, out, fmt.Sprintf("Start %s?", s.Name)) {
			return nil
		}
		if err := m.Start(ctx, s.Name); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Orca web UI for %s:\n", s.Name)
	return printURL(cmd, m, s.Name)
}

// runCreate hands over to `orcasm create` (the Wizard, then provisioning).
func runCreate(cmd *cobra.Command) error {
	create, _, err := cmd.Root().Find([]string{"create"})
	if err != nil || create == cmd.Root() {
		return fmt.Errorf("create command not found")
	}
	create.SetContext(cmd.Context())
	return create.RunE(create, nil)
}

// confirm asks a yes/no question defaulting to yes; no answer (EOF) is no.
func confirm(in *bufio.Reader, out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [Y/n] ", question)
	answer, err := in.ReadString('\n')
	if err != nil && answer == "" {
		fmt.Fprintln(out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true
	}
	return false
}
