package repository

import (
	"context"
	"fmt"
)

// RegisterInOrca registers the Repository at guestPath as an Orca project.
// It runs as orcaUser and needs Orca's runtime (`orca-ide serve`) running.
func RegisterInOrca(ctx context.Context, sb Sandbox, orcaUser, guestPath string) error {
	if _, err := sb.Exec(ctx, orcaUser, "orca-ide", "repo", "add", "--path", guestPath); err != nil {
		return fmt.Errorf("register %s in Orca: %w", guestPath, err)
	}
	return nil
}
