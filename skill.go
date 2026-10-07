package toolbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChristopherDavenport/agentskill"
	"github.com/ChristopherDavenport/agenttool"
	"github.com/ChristopherDavenport/agenttool/cli"
)

// skill is the bundle as an Agent Skill. Its body is a title, the
// description and the usage cli renders for the commands that are not
// hidden, each followed by its examples. allowed-tools pre-approves
// each command the author marked, as Bash(<name> <command> *), the
// form Claude Code's permission dialog writes. Its metadata is the
// version and the sha256 of the body, which is exactly what the model
// reads.
func (b Bundle) skill() (*agentskill.Skill, error) {
	body, err := b.body()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(body))
	meta := map[string]string{"digest": hex.EncodeToString(sum[:])}
	if v := b.version(); v != "" {
		meta["version"] = v
	}
	return &agentskill.Skill{
		DirName:      b.Name,
		Name:         b.Name,
		Description:  b.Description,
		AllowedTools: strings.Join(b.allowed(), " "),
		Metadata:     meta,
		Body:         body,
	}, nil
}

// allowed is the allowed-tools rule of each pre-approved command, in
// the bundle's order. A hidden command is never pre-approved, since the
// skill does not teach it.
func (b Bundle) allowed() []string {
	var rules []string
	for _, t := range b.Tools {
		if t.preApproved && !t.hidden {
			rules = append(rules, fmt.Sprintf("Bash(%s %s *)", b.Name, t.tool.Name()))
		}
	}
	return rules
}

func (b Bundle) body() (string, error) {
	var tools []Tool
	for _, t := range b.Tools {
		if !t.hidden {
			tools = append(tools, t)
		}
	}
	cmds, err := cli.Commands(b.visible())
	if err != nil {
		return "", err
	}

	// cli.Markdown renders the calling section, then each command after
	// it. Rendering it with no commands, and then with each one, gives
	// the pieces to put the examples between.
	head := cli.Markdown(b.Name, nil)
	var s strings.Builder
	fmt.Fprintf(&s, "\n# %s\n\n", b.Name)
	if d := strings.TrimSpace(b.Description); d != "" {
		s.WriteString(d)
		s.WriteString("\n\n")
	}
	s.WriteString(head)
	for i, c := range cmds {
		section, ok := strings.CutPrefix(cli.Markdown(b.Name, []cli.Command{c}), head)
		if !ok {
			return "", errors.New("cli.Markdown no longer renders the calling section before the commands")
		}
		s.WriteString(section)
		for _, ex := range tools[i].examples {
			caption := strings.TrimSpace(ex.caption)
			if caption == "" {
				caption = "Example"
			}
			fmt.Fprintf(&s, "\n%s:\n\n```sh\n%s %s\n```\n", strings.TrimSuffix(caption, "."), b.Name, ex.args)
		}
	}
	return s.String(), nil
}

// exportSkill is the export-skill command: it writes the skill to
// <dir>/<name>/SKILL.md, and says what it pre-approved and what it
// thinks the author should know.
func (b Bundle) exportSkill(args []string, argv0 string, stdout, stderr io.Writer) int {
	const usage = "usage: %s export-skill [--force] <dir>\n"
	force := false
	var dirs []string
	for _, a := range args {
		switch a {
		case "--force", "-force":
			force = true
		case "--help", "-help", "-h":
			fmt.Fprintf(stdout, usage, b.Name)
			return cli.ExitOK
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(stderr, "%s: export-skill: unknown option %s\n"+usage, b.Name, a, b.Name)
				return cli.ExitUsage
			}
			dirs = append(dirs, a)
		}
	}
	if len(dirs) != 1 {
		fmt.Fprintf(stderr, usage, b.Name)
		return cli.ExitUsage
	}

	s, err := b.skill()
	if err != nil {
		fmt.Fprintf(stderr, "%s: export-skill: %v\n", b.Name, err)
		return cli.ExitFailed
	}
	problems := s.Validate()
	if agentskill.HasErrors(problems) {
		fmt.Fprintf(stderr, "%s: export-skill: the skill is not valid: %s\n", b.Name, problemList(problems))
		return cli.ExitFailed
	}
	data, err := agentskill.Encode(s)
	if err != nil {
		fmt.Fprintf(stderr, "%s: export-skill: %v\n", b.Name, err)
		return cli.ExitFailed
	}
	path := filepath.Join(dirs[0], b.Name, "SKILL.md")
	if err := writeSkill(path, data, force); err != nil {
		fmt.Fprintf(stderr, "%s: export-skill: %v\n", b.Name, err)
		return cli.ExitFailed
	}

	for _, p := range problems {
		fmt.Fprintf(stderr, "%s: warning: %s\n", b.Name, p.Message)
	}
	for _, w := range b.warnings() {
		fmt.Fprintf(stderr, "%s: warning: %s\n", b.Name, w)
	}
	if base := strings.TrimSuffix(filepath.Base(argv0), ".exe"); argv0 != "" && base != b.Name {
		fmt.Fprintf(stderr, "%s: note: this binary was run as %s, and the skill calls it %s; install it under that name\n", b.Name, base, b.Name)
	}

	fmt.Fprintf(stdout, "wrote %s\n", path)
	if rules := b.allowed(); len(rules) > 0 {
		fmt.Fprintf(stdout, "pre-approved in allowed-tools, run without asking in the turn that uses the skill:\n")
		for _, r := range rules {
			fmt.Fprintf(stdout, "  %s\n", r)
		}
	} else {
		fmt.Fprintf(stdout, "no command is pre-approved\n")
	}
	return cli.ExitOK
}

// writeSkill writes a SKILL.md, refusing to replace one unless force.
func writeSkill(path string, data []byte, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s exists; give --force to replace it", path)
	}
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// warnings names each tool that looks as if it keeps state across
// calls, which one call per process loses, unless its author said so
// with Stateful.
func (b Bundle) warnings() []string {
	var out []string
	for _, t := range b.Tools {
		if t.stateful {
			continue
		}
		var why []string
		if r := agenttool.ResourceOf(t.tool); r != "" {
			why = append(why, fmt.Sprintf("declares the resource %q", r))
		}
		if agenttool.IsSequential(t.tool) {
			why = append(why, "is sequential")
		}
		if _, ok := t.tool.(io.Closer); ok {
			why = append(why, "owns something it closes")
		}
		if len(why) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s, as a tool that keeps state across calls does, and each call here is a process of its own; leave it out of the bundle, or mark it toolbundle.Stateful() if it keeps nothing a call needs from the last",
			t.tool.Name(), strings.Join(why, " and ")))
	}
	return out
}

func problemList(problems []agentskill.Problem) string {
	var msgs []string
	for _, p := range problems {
		if p.Severity == agentskill.Error {
			msgs = append(msgs, p.Message)
		}
	}
	return strings.Join(msgs, "; ")
}
