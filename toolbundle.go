// Package toolbundle builds one program from a set of tools: a binary
// whose commands are the tools, run through [cli.Runner], and which
// writes the Agent Skill that teaches a model to call it. It is the way
// out for a host that allows no MCP server but gives the model a shell,
// a permission layer over shell commands and skills.
//
//	func main() {
//		os.Exit(toolbundle.Main(context.Background(), toolbundle.Bundle{
//			Name:        "file-tools",
//			Description: "Read, search and edit files in the working tree. Use when …",
//			Tools: []toolbundle.Tool{
//				toolbundle.Use(ReadFile, toolbundle.PreApproved(),
//					toolbundle.Example("read_file --path go.mod", "Read a file")),
//				toolbundle.Use(WriteFile),
//			},
//		}, os.Args))
//	}
//
// The tools need nothing to be bundled. What a tool needs to say about
// the bundle, whether its command is pre-approved, its examples, that
// it keeps state, is said where the bundle is built, with [Use], and
// never on the tool.
//
// One call is one process, so a tool that keeps state across calls, a
// persistent shell or a container session, loses it at exit. Such a
// tool is better left out of the bundle; export-skill warns about one
// that looks like it, unless [Stateful] says the author knows.
package toolbundle

import (
	"github.com/ChristopherDavenport/agenttool"
)

// Bundle is a set of tools as one program, and the skill that teaches
// a model to call it.
type Bundle struct {
	// Name is the program's name, the name the binary is installed
	// under, and the skill's name. It must be a valid skill name:
	// lowercase letters, digits and hyphens, at most 64 characters,
	// with no leading, trailing or doubled hyphen.
	Name string
	// Description is the skill's description: what the tools do and
	// when to use them, which is what a model reads to choose the
	// skill.
	Description string
	// Version is the program's version, printed by the version command
	// and recorded in the skill's metadata. Empty, it is the main
	// module's version from the build information.
	Version string
	// Tools are the program's commands, in the order the skill lists
	// them.
	Tools []Tool
}

// Tool is a tool in a bundle, with what the bundle needs to know about
// it. It is built with [Use] or [Uses].
type Tool struct {
	tool        agenttool.Tool
	preApproved bool
	stateful    bool
	hidden      bool
	examples    []example
}

// example is a worked command line, the words after the program's
// name as the author wrote them.
type example struct {
	args    string
	caption string
}

// An Option says something about a tool in a bundle.
type Option func(*Tool)

// Use puts tool in a bundle with opts.
func Use(tool agenttool.Tool, opts ...Option) Tool {
	t := Tool{tool: tool}
	for _, opt := range opts {
		opt(&t)
	}
	return t
}

// Uses puts every tool of set in a bundle with no options:
// toolbundle.Uses(set...).
func Uses(set ...agenttool.Tool) []Tool {
	tools := make([]Tool, len(set))
	for i, t := range set {
		tools[i] = Use(t)
	}
	return tools
}

// PreApproved lists the tool's command in the skill's allowed-tools,
// so a host that honours the field runs it without asking. It is the
// author's choice, made per tool, and never inferred from the tool's
// read-only hint: a hint may make a policy stricter and may not alone
// allow a call. export-skill prints every command it pre-approved.
func PreApproved() Option {
	return func(t *Tool) { t.preApproved = true }
}

// Example adds a worked command line to the tool's section of the
// skill. args is what follows the program's name, the command first:
// "read_file --path go.mod". The program parses it against the command
// at startup and refuses to run if it does not parse, so an example
// cannot teach a flag that does not exist. An example gives its JSON
// argument inline, and quotes any word holding a character other than
// a letter, a digit or one of -_./:=,@+%.
func Example(args, caption string) Option {
	return func(t *Tool) { t.examples = append(t.examples, example{args: args, caption: caption}) }
}

// Stateful acknowledges that the tool keeps state across calls, which
// one call per process loses. It silences export-skill's warning about
// a tool that declares a resource, is sequential or is a closer; it
// does not change how the tool runs.
func Stateful() Option {
	return func(t *Tool) { t.stateful = true }
}

// Hidden leaves the tool out of the skill and out of help, for a
// maintenance command a model should not reach for. It stays runnable
// by name.
func Hidden() Option {
	return func(t *Tool) { t.hidden = true }
}
