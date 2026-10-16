// Command compatfile reads and updates the compatibility file for CI's Orca
// release watcher (.github/workflows/orca-watch.yml, #24):
//
//	go run ./internal/compat/compatfile tested FILE ORCA       # exit 0 if listed, 1 if not
//	go run ./internal/compat/compatfile newest FILE            # newest listed Orca
//	go run ./internal/compat/compatfile mark FILE ORCA ORCASM  # record a pass
//
// mark prints "added" or "unchanged" so the workflow knows whether to commit.
package main

import (
	"fmt"
	"os"

	"github.com/Betree/Orcasm/internal/compat"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "compatfile:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: compatfile tested|newest|mark FILE [ORCA [ORCASM]]")
	}
	cmd, path := args[0], args[1]
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	switch {
	case cmd == "tested" && len(args) == 3:
		f, err := compat.Parse(raw)
		if err != nil {
			return err
		}
		for _, e := range f.Releases {
			if compat.Compare(e.Version, args[2]) == 0 {
				return nil
			}
		}
		os.Exit(1)
	case cmd == "newest" && len(args) == 2:
		f, err := compat.Parse(raw)
		if err != nil {
			return err
		}
		v, _ := f.Orca("dev") // a development build sees every entry
		fmt.Println(v)
	case cmd == "mark" && len(args) == 4:
		out, added, err := compat.MarkTested(raw, compat.Entry{Version: args[2], MinOrcasm: args[3]})
		if err != nil {
			return err
		}
		if !added {
			fmt.Println("unchanged")
			return nil
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
		fmt.Println("added")
	default:
		return fmt.Errorf("unknown or malformed command %q", args)
	}
	return nil
}
