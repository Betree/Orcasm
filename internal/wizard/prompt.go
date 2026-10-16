package wizard

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// prompter asks the user, pre-filled with the current answers.
type prompter struct {
	in  io.Reader
	out io.Writer
}

func (p prompter) run(groups ...*huh.Group) error {
	if isTerminal(p.in) {
		return huh.NewForm(groups...).WithInput(p.in).WithOutput(p.out).Run()
	}
	// Accessible mode buffers its input per field; feed it a byte at a time
	// so a piped input isn't swallowed by the first question.
	return huh.NewForm(groups...).WithInput(byteReader{p.in}).WithOutput(p.out).
		WithAccessible(true).Run()
}

type byteReader struct{ r io.Reader }

func (b byteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return b.r.Read(p)
}

// isTerminal tells whether r is an interactive terminal; otherwise the
// Wizard falls back to huh's line-by-line accessible mode.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (p prompter) projectPath(projectPath *string) error {
	return p.run(huh.NewGroup(
		huh.NewInput().Title("Project path").Value(projectPath),
	))
}

// sandbox asks for everything but the Project. s lists what was found
// in the projectPath; available harnesses are those on the host.
func (p prompter) sandbox(cfg *Config, h Host, s scan) error {
	cpus := strconv.Itoa(cfg.Resources.CPUs)
	mem := strconv.Itoa(cfg.Resources.MemoryGiB)
	root := strconv.Itoa(cfg.Resources.RootDiskGiB)
	docker := strconv.Itoa(cfg.Resources.DockerDiskGiB)
	port := strconv.Itoa(cfg.HostPort)

	fields := []huh.Field{
		huh.NewInput().Title("Sandbox name").Value(&cfg.Name),
		huh.NewInput().Title("CPUs").Description(fmt.Sprintf("host has %d", h.CPUs)).Value(&cpus).Validate(isInt),
		huh.NewInput().Title("Memory (GiB)").Description(fmt.Sprintf("host has %d GiB", h.MemoryGiB)).Value(&mem).Validate(isInt),
		huh.NewInput().Title("Root disk (GiB)").Value(&root).Validate(isInt),
		huh.NewInput().Title("Docker disk (GiB, sparse)").Value(&docker).Validate(isInt),
		huh.NewInput().Title("Host port for Orca's web UI").Value(&port).Validate(isInt),
		huh.NewMultiSelect[string]().Title("Harnesses to install").
			Options(huh.NewOptions(SupportedHarnesses...)...).Value(&cfg.Harnesses),
	}
	if len(s.composeFiles) > 0 {
		fields = append(fields, huh.NewMultiSelect[string]().Title("Compose files making up the Stack").
			Options(huh.NewOptions(s.composeFiles...)...).Value(&cfg.Project.ComposeFiles))
	}
	if err := p.run(huh.NewGroup(fields...)); err != nil {
		return err
	}
	cfg.Resources = Resources{CPUs: atoi(cpus), MemoryGiB: atoi(mem), RootDiskGiB: atoi(root), DockerDiskGiB: atoi(docker)}
	cfg.HostPort = atoi(port)

	var project []huh.Field
	services, _ := composeServices(cfg.ProjectPath, cfg.Project.ComposeFiles)
	if len(services) > 0 {
		webPort := ""
		if cfg.Project.WebPort > 0 {
			webPort = strconv.Itoa(cfg.Project.WebPort)
		}
		project = append(project,
			huh.NewSelect[string]().Title("Workbench service").
				Options(append([]huh.Option[string]{huh.NewOption("(generic fallback)", "")}, huh.NewOptions(services...)...)...).
				Value(&cfg.Project.Workbench),
			huh.NewSelect[string]().Title("Web service").
				Options(append([]huh.Option[string]{huh.NewOption("(none)", "")}, huh.NewOptions(services...)...)...).
				Value(&cfg.Project.WebService),
			huh.NewInput().Title("Web service port").Description("container port; leave empty without a web service").
				Value(&webPort).Validate(func(v string) error {
				if v == "" {
					return nil
				}
				return isInt(v)
			}),
		)
		defer func() { cfg.Project.WebPort = atoi(webPort) }()
	}
	if len(s.ignored) > 0 {
		project = append(project, huh.NewMultiSelect[string]().Title("Gitignored files to bring into the Sandbox").
			Options(huh.NewOptions(s.ignored...)...).Value(&cfg.Project.BringFiles))
	}
	if len(project) == 0 {
		return nil
	}
	return p.run(huh.NewGroup(project...))
}

// gitOption is the sharing choice for the git identity; the others are
// Secret names.
const gitOption = "git identity"

// share asks which of the host's tokens and git identity to hand to the
// Sandbox, pre-selected with the current answers.
func (p prompter) share(cfg *Config, secrets []Secret, git GitIdentity) error {
	var options []huh.Option[string]
	for _, s := range secrets {
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s from %s, sent only to %s", s.Name, s.Source, strings.Join(s.Hosts, ", ")), s.Name))
	}
	if git != (GitIdentity{}) {
		options = append(options, huh.NewOption(fmt.Sprintf("git identity: %s <%s>", git.Name, git.Email), gitOption))
	}
	if len(options) == 0 {
		return nil
	}
	var picked []string
	for _, s := range cfg.Secrets {
		picked = append(picked, s.Name)
	}
	if cfg.Git != (GitIdentity{}) {
		picked = append(picked, gitOption)
	}
	err := p.run(huh.NewGroup(huh.NewMultiSelect[string]().
		Title("Share with the Sandbox").
		Description("Agents only see placeholders for tokens; the real value is added on requests to the listed hosts.").
		Options(options...).Value(&picked)))
	if err != nil {
		return err
	}
	cfg.Secrets, cfg.Git = []Secret{}, GitIdentity{}
	for _, name := range picked {
		if name == gitOption {
			cfg.Git = git
		} else {
			cfg.Secrets = append(cfg.Secrets, Secret{Name: name})
		}
	}
	return nil
}

// linkSSHConfig asks for consent to add orcasm's Include to ~/.ssh/config.
func (p prompter) linkSSHConfig(link *bool) error {
	return p.run(huh.NewGroup(huh.NewConfirm().
		Title("Add one Include line to ~/.ssh/config so VS Code Remote-SSH lists your Sandboxes?").
		Description("`orcasm ssh` and the orcasm-<name> git remote work either way.").
		Value(link)))
}

// isInt accepts a number, or nothing: in line-by-line mode an empty answer
// keeps the default, and an emptied field otherwise fails validation.
func isInt(v string) error {
	if v == "" {
		return nil
	}
	if _, err := strconv.Atoi(v); err != nil {
		return fmt.Errorf("enter a number")
	}
	return nil
}

func atoi(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}
