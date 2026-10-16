package sandbox

// StackCommand is the `stack` command Provision installs in the Sandbox.
func StackCommand() string { return stackScript }

// SetMigrations replaces the registered migrations, so tests run their own.
// It returns a func restoring the real ones.
func SetMigrations(ms []Migration) (restore func()) {
	old := migrations
	migrations = ms
	return func() { migrations = old }
}

// Migrations returns the registered migrations.
func Migrations() []Migration { return migrations }
