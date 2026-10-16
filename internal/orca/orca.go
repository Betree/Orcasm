// Package orca talks to Orca's runtime from inside the Sandbox, for what
// `orca-ide` has no command for (docs/research/orca.md, "Hooks without
// committed orca.yaml"): one JSON request line over the runtime's unix
// socket, as the CLI itself does.
package orca

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Runtime is a running Orca runtime (`orca-ide serve`) of one user.
type Runtime struct {
	// UserData is Orca's user data dir, holding orca-runtime.json.
	UserData string
}

// DefaultRuntime is the current user's runtime, where orca-ide finds it.
func DefaultRuntime() (Runtime, error) {
	if dir := os.Getenv("ORCA_USER_DATA_PATH"); dir != "" {
		return Runtime{UserData: dir}, nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{UserData: filepath.Join(config, "orca")}, nil
}

// HookEndpointFile is where Orca writes its agent hook server's current
// port and token (ORCA_AGENT_HOOK_PORT=...), rewritten on each start.
func (r Runtime) HookEndpointFile() string {
	return filepath.Join(r.UserData, "agent-hooks", "endpoint.env")
}

// HookSettings are a repo's hook settings in Orca's own state (no
// orca.yaml); an update replaces them whole.
type HookSettings struct {
	Mode                    string  `json:"mode,omitempty"`
	SetupRunPolicy          string  `json:"setupRunPolicy,omitempty"`
	SetupAgentStartupPolicy string  `json:"setupAgentStartupPolicy,omitempty"`
	Scripts                 Scripts `json:"scripts"`
}

// Scripts are a repo's setup and archive hooks: Bash run as the Orca user
// with ORCA_ROOT_PATH, ORCA_WORKTREE_PATH and ORCA_WORKSPACE_NAME set.
type Scripts struct {
	Setup   string `json:"setup"`
	Archive string `json:"archive"`
}

// RepoUpdates are the repo settings UpdateRepo changes; zero fields are
// left alone.
type RepoUpdates struct {
	HookSettings *HookSettings `json:"hookSettings,omitempty"`
	// WorktreeBasePath is where new worktrees go, as <base>/<repo>/<name>.
	WorktreeBasePath string `json:"worktreeBasePath,omitempty"`
}

// UpdateRepo changes the settings of the repo registered at path.
func (r Runtime) UpdateRepo(ctx context.Context, path string, updates RepoUpdates) error {
	return r.call(ctx, "repo.update", map[string]any{"repo": "path:" + path, "updates": updates})
}

type metadata struct {
	Transports []struct {
		Kind     string `json:"kind"`
		Endpoint string `json:"endpoint"`
	} `json:"transports"`
	AuthToken string `json:"authToken"`
}

type response struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r Runtime) call(ctx context.Context, method string, params any) error {
	metaFile := filepath.Join(r.UserData, "orca-runtime.json")
	raw, err := os.ReadFile(metaFile)
	if err != nil {
		return fmt.Errorf("Orca's runtime is not running: %w", err)
	}
	var meta metadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("%s: %w", metaFile, err)
	}
	socket := ""
	for _, t := range meta.Transports {
		if t.Kind == "unix" {
			socket = t.Endpoint
		}
	}
	if socket == "" {
		return fmt.Errorf("%s lists no unix socket", metaFile)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("Orca's runtime: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	req, err := json.Marshal(map[string]any{
		"id": hex.EncodeToString(id), "authToken": meta.AuthToken, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("Orca's runtime: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("Orca's runtime: no answer to %s: %w", method, err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("Orca's runtime: %s: %w", method, err)
	}
	if !resp.OK {
		if resp.Error != nil {
			return fmt.Errorf("Orca's runtime: %s: %s (%s)", method, resp.Error.Message, resp.Error.Code)
		}
		return fmt.Errorf("Orca's runtime: %s failed", method)
	}
	return nil
}
