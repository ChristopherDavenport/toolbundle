package toolbundle

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/ChristopherDavenport/agentskill"
	"github.com/ChristopherDavenport/agenttool"
	"github.com/ChristopherDavenport/agenttool/cli"
)

// The program's own commands, recognised only as the first argument.
// help and schema are cli's; a tool may be named as none of them.
const (
	cmdExportSkill = "export-skill"
	cmdVersion     = "version"
)

var reserved = []string{cmdExportSkill, cmdVersion, "help", "schema"}

// Main runs the program the bundle describes with args, the command
// line with the program's name first as in [os.Args], and returns the
// exit status for [os.Exit]:
//
//	<name> export-skill [--force] <dir>
//	<name> version
//	<name> [--ask] <cli.Runner's command line>
//
// export-skill writes the skill to <dir>/<name>/SKILL.md, and version
// prints the version. Anything else is one call of one tool, run by
// [cli.Runner] with the exit statuses it documents. --ask, given first,
// asks a person at the terminal any question a tool asks, through
// [cli.Prompt]; without it a question takes cli's protocol for a
// model: the call ends with [cli.ExitNeedsAnswer] and the question on
// stdout. A terminal does not say who is at it, so the default is the
// one that is safe for a model.
//
// Before anything runs, Main refuses a bundle that could not export a
// valid skill, a tool named as one of the program's commands, and an
// example that does not parse, with [cli.ExitUsage]. It stops the call
// on an interrupt, and closes the tools when it is done with
// [agenttool.Set.Close], whatever ran; a failed close is reported on
// stderr and leaves the exit status as it was, since the call has
// already happened.
func Main(ctx context.Context, b Bundle, args []string) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	return b.main(ctx, args, os.Stdin, os.Stdout, os.Stderr)
}

func (b Bundle) main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	defer func() {
		if err := b.set().Close(); err != nil {
			fmt.Fprintf(stderr, "%s: closing the tools: %v\n", b.Name, err)
		}
	}()
	if err := b.check(ctx); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", b.Name, err)
		return cli.ExitUsage
	}

	var argv0 string
	if len(args) > 0 {
		argv0, args = args[0], args[1:]
	}
	if len(args) > 0 {
		switch args[0] {
		case cmdExportSkill:
			return b.exportSkill(args[1:], argv0, stdout, stderr)
		case cmdVersion:
			if len(args) > 1 {
				fmt.Fprintf(stderr, "%s: %s takes no arguments\n", b.Name, cmdVersion)
				return cli.ExitUsage
			}
			fmt.Fprintf(stdout, "%s %s\n", b.Name, b.version())
			return cli.ExitOK
		}
	}

	r := cli.Runner{Name: b.Name, Tools: b.visible(), Stdin: stdin, Stdout: stdout, Stderr: stderr}
	if len(args) > 0 && (args[0] == "--ask" || args[0] == "-ask") {
		r.Ask = cli.Prompt(stdin, stderr)
		args = args[1:]
	}
	if b.isHidden(command(args)) {
		r.Tools = b.set()
	}
	return r.Run(ctx, args)
}

// check refuses a bundle the program should not run: one that could
// not export a valid skill, a tool missing or named as a command of the
// program's own, a set cli refuses, or an example that does not parse.
func (b Bundle) check(ctx context.Context) error {
	if b.Name == "" {
		return errors.New("the bundle has no name")
	}
	for i, t := range b.Tools {
		if t.tool == nil {
			return fmt.Errorf("tool %d is nil; build it with toolbundle.Use", i)
		}
		for _, r := range reserved {
			if t.tool.Name() == r {
				return fmt.Errorf("a tool is named %q, which is a command of the program's own", r)
			}
		}
	}
	all, err := cli.Commands(b.set())
	if err != nil {
		return err
	}
	for i, t := range b.Tools {
		for _, ex := range t.examples {
			if err := checkExample(ctx, b.Name, all[i], ex.args); err != nil {
				return fmt.Errorf("example %q of %s: %w", ex.args, all[i].Name, err)
			}
		}
	}
	s, err := b.skill()
	if err != nil {
		return err
	}
	if problems := s.Validate(); agentskill.HasErrors(problems) {
		return fmt.Errorf("the bundle cannot export a valid skill: %s", problemList(problems))
	}
	return nil
}

// set is every tool of the bundle, hidden ones included.
func (b Bundle) set() agenttool.Set {
	set := make(agenttool.Set, 0, len(b.Tools))
	for _, t := range b.Tools {
		if t.tool != nil {
			set = append(set, t.tool)
		}
	}
	return set
}

// visible is the tools the skill and help show.
func (b Bundle) visible() agenttool.Set {
	var set agenttool.Set
	for _, t := range b.Tools {
		if !t.hidden {
			set = append(set, t.tool)
		}
	}
	return set
}

func (b Bundle) isHidden(name string) bool {
	for _, t := range b.Tools {
		if t.hidden && t.tool.Name() == name {
			return true
		}
	}
	return false
}

// version is the bundle's version, or the main module's when it sets
// none: "(devel)" for a binary built from a checkout.
func (b Bundle) version() string {
	if b.Version != "" {
		return b.Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return ""
}

// command is the command a cli.Runner command line names, after the
// program options cli reads before it, or "" when it names none or
// the options do not parse. It decides only whether the runner is
// given the hidden tools; the runner parses the line itself, so an
// option cli adds later costs a hidden tool its reach behind that
// option and nothing else.
func command(args []string) string {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Func("answer", "", func(string) error { return nil })
	fs.String("record", "", "")
	fs.String("out", "", "")
	if fs.Parse(args) != nil || fs.NArg() == 0 {
		return ""
	}
	return fs.Arg(0)
}
