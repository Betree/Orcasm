package wizard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Secret is a host token handed to the Sandbox as a microsandbox secret: the
// guest only sees a placeholder in $Name, and msb swaps in the real value on
// requests to Hosts. The Config holds where the value comes from, never the
// value itself (see ADR 0001).
type Secret struct {
	// Name is the environment variable holding the placeholder in the guest
	// (and the one msb reads the value from on the host).
	Name string `toml:"name"`
	// Hosts are the only hosts the value is sent to.
	Hosts  []string `toml:"hosts,omitempty"`
	Source Source   `toml:"source,omitempty"`
}

// Source says where on the host a Secret's value is read from; exactly one
// field is set.
type Source struct {
	// Env is a host environment variable.
	Env string `toml:"env,omitempty"`
	// Command is a host command printing the value, e.g. `gh auth token`.
	Command []string `toml:"command,omitempty"`
	// OpenCodeAuth is a provider in OpenCode's auth.json; only API-key
	// entries are read, never OAuth logins.
	OpenCodeAuth string `toml:"opencode_auth,omitempty"`
}

// String describes where the value comes from, for the user.
func (src Source) String() string {
	switch {
	case src.Env != "":
		return "$" + src.Env
	case len(src.Command) > 0:
		return "`" + strings.Join(src.Command, " ") + "`"
	case src.OpenCodeAuth != "":
		return "OpenCode's " + src.OpenCodeAuth + " key"
	}
	return "nowhere"
}

// SecretValue reads s's value from the host. It's called when the Sandbox is
// started, so the value never goes through the Config.
func (h Host) SecretValue(s Secret) (string, error) {
	var v string
	switch src := s.Source; {
	case src.Env != "":
		v = h.getenv(src.Env)
	case len(src.Command) > 0:
		if h.Output == nil {
			return "", fmt.Errorf("secret %s: cannot run %s", s.Name, src.Command[0])
		}
		out, err := h.Output(src.Command[0], src.Command[1:]...)
		if err != nil {
			return "", fmt.Errorf("secret %s: %w", s.Name, err)
		}
		v = strings.TrimSpace(out)
	case src.OpenCodeAuth != "":
		v = h.openCodeAPIKey(src.OpenCodeAuth)
	default:
		return "", fmt.Errorf("secret %s has no source", s.Name)
	}
	if v == "" {
		return "", fmt.Errorf("secret %s: no value on the host", s.Name)
	}
	return v, nil
}

func (h Host) getenv(key string) string {
	if h.Getenv == nil {
		return ""
	}
	return h.Getenv(key)
}

// openCodeAPIKey is the API key OpenCode stores for provider, or "".
func (h Host) openCodeAPIKey(provider string) string {
	if h.OpenCodeAuthFile == "" {
		return ""
	}
	data, err := os.ReadFile(h.OpenCodeAuthFile)
	if err != nil {
		return ""
	}
	var auth map[string]struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if json.Unmarshal(data, &auth) != nil || auth[provider].Type != "api" {
		return ""
	}
	return auth[provider].Key
}

// apiKeys are the API keys orcasm knows the hosts of: the environment
// variable the harnesses read, and the matching OpenCode provider ("" when
// OpenCode doesn't use that variable).
var apiKeys = []struct {
	env, openCode string
	hosts         []string
}{
	{"ANTHROPIC_API_KEY", "anthropic", []string{"api.anthropic.com"}},
	{"OPENAI_API_KEY", "openai", []string{"api.openai.com"}},
	{"OPENROUTER_API_KEY", "openrouter", []string{"openrouter.ai"}},
	{"GROQ_API_KEY", "groq", []string{"api.groq.com"}},
	{"MISTRAL_API_KEY", "mistral", []string{"api.mistral.ai"}},
	{"DEEPSEEK_API_KEY", "deepseek", []string{"api.deepseek.com"}},
	{"XAI_API_KEY", "xai", []string{"api.x.ai"}},
	{"GEMINI_API_KEY", "", []string{"generativelanguage.googleapis.com"}},
}

// candidateSecrets are every Secret orcasm knows how to fetch from a host,
// most preferred first for a given Name; whether the host has a value is
// only checked when needed.
func candidateSecrets() []Secret {
	all := []Secret{{
		Name:   "GH_TOKEN",
		Hosts:  []string{"github.com", "api.github.com", "uploads.github.com"},
		Source: Source{Command: []string{"gh", "auth", "token"}},
	}}
	for _, k := range apiKeys {
		all = append(all, Secret{Name: k.env, Hosts: k.hosts, Source: Source{Env: k.env}})
		if k.openCode != "" {
			all = append(all, Secret{Name: k.env, Hosts: k.hosts, Source: Source{OpenCodeAuth: k.openCode}})
		}
	}
	return all
}

// availableSecret is the first candidate named name that has a value on h.
func availableSecret(h Host, name string) (Secret, bool) {
	for _, s := range candidateSecrets() {
		if s.Name != name {
			continue
		}
		if _, err := h.SecretValue(s); err == nil {
			return s, true
		}
	}
	return Secret{}, false
}

// availableSecrets are the Secrets the host has a value for, one per Name.
func availableSecrets(h Host) []Secret {
	var out []Secret
	for _, name := range secretNames() {
		if s, ok := availableSecret(h, name); ok {
			out = append(out, s)
		}
	}
	return out
}

// hostGitIdentity is the git identity configured for projectPath on the host.
func hostGitIdentity(h Host, projectPath string) GitIdentity {
	get := func(key string) string {
		if h.Output == nil {
			return ""
		}
		out, err := h.Output("git", "-C", projectPath, "config", key)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	id := GitIdentity{Name: get("user.name"), Email: get("user.email")}
	if id.Name == "" || id.Email == "" {
		return GitIdentity{}
	}
	return id
}

// resolveSecrets turns approved secrets (only their Name matters) into the
// specs found on the host.
func resolveSecrets(h Host, approved []Secret) ([]Secret, error) {
	var out []Secret
	var errs []error
	for _, a := range approved {
		if slices.ContainsFunc(out, func(s Secret) bool { return s.Name == a.Name }) {
			continue
		}
		s, ok := availableSecret(h, a.Name)
		if !ok {
			errs = append(errs, fmt.Errorf("secret %s: not found on the host (known: %s)", a.Name, strings.Join(secretNames(), ", ")))
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

func secretNames() []string {
	var names []string
	for _, s := range candidateSecrets() {
		if !slices.Contains(names, s.Name) {
			names = append(names, s.Name)
		}
	}
	return names
}
