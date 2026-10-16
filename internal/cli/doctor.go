package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/doctor"
)

func init() { register(newDoctorCmd) }

func newDoctorCmd(BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that this host can run a Sandbox",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := doctor.Run(doctor.RealHost{})
			out := cmd.OutOrStdout()
			for _, c := range report.Checks {
				status := "ok  "
				if !c.OK {
					status = "FAIL"
				}
				fmt.Fprintf(out, "%s  %-6s %s\n", status, c.Name, c.Detail)
			}
			if !report.OK() {
				return errors.New("host is not ready, see the failed checks above")
			}
			return nil
		},
	}
}
