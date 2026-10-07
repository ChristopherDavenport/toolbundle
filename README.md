# toolbundle

A set of [agenttool](https://github.com/ChristopherDavenport/agenttool)
tools as one command-line program, which runs them and writes the
[Agent Skill](https://agentskills.io/specification) that teaches a
model to call it.

A set of tools reaches a model in process, handed to a loop, or over
MCP. Some hosts allow no MCP server, but still give the model a shell,
a permission layer over shell commands and skills. A bundle is the way
out for those. It is the weakest of the three and is not meant to
compete with the others: use it where they are not permitted.

```go
func main() {
	os.Exit(toolbundle.Main(context.Background(), toolbundle.Bundle{
		Name:        "file-tools",
		Description: "Read, search and write files in the working tree. Use when a task needs the contents of a file or a change to one.",
		Tools: []toolbundle.Tool{
			toolbundle.Use(ReadFile, toolbundle.PreApproved(),
				toolbundle.Example("read_file --path go.mod", "Read a file")),
			toolbundle.Use(Search, toolbundle.PreApproved()),
			toolbundle.Use(WriteFile),
		},
	}, os.Args))
}
```

```sh
go install ./cmd/file-tools          # installed under the bundle's name
file-tools export-skill ~/.claude/skills
file-tools read_file --path go.mod
```

The tools need nothing to be bundled. Parsing, running and the usage
text are [`agenttool/cli`](https://pkg.go.dev/github.com/ChristopherDavenport/agenttool/cli)'s;
this module adds the program around it and the skill.

The same binary can also be the MCP server, for the hosts that take
one: add the `mcp` command from the nested module
[`toolbundle/mcp`](#serving-mcp-from-the-same-binary) and run
`file-tools mcp`.

## The program

```
<name> export-skill [--force] <dir>
<name> version
<name> <command of Bundle.Commands> [<arg>]...
<name> [--ask] [--answer <reply>]... [--record <file>] [--out <dir>] <command> [--<param> <value>]... [<json> | -]
<name> help [<command>]
<name> schema <command>
```

- `export-skill` writes the skill to `<dir>/<name>/SKILL.md`, since a
  skill's directory is named as the skill. It refuses to replace a
  `SKILL.md` without `--force`, and prints every command it
  pre-approved.
- `version` prints the bundle's `Version`, or the main module's from
  the build information when it sets none.
- Anything else is one call of one tool, with `cli`'s exit statuses:
  0 success, 1 the tool's error, 2 a call that did not run, 3 a
  question nobody answered.
- `--ask`, given first, asks a person at the terminal any question a
  tool asks. Without it, a question ends the call with exit 3 and the
  question on stdout, for the caller to ask and run the call again
  with `--answer`. A terminal does not say who is at it, so the
  default is the one that is safe for a model.

`export-skill`, `version` and the bundle's `Commands` are recognised
only as the first argument. `Main` refuses to start, with exit 2, a
bundle whose name is not a valid skill name, whose skill would not
validate, which names a tool or a command as a command of the
program's own (`export-skill`, `version`, `help`, `schema`, or one of
`Commands`), or whose example does not parse. It stops a call on
an interrupt, and closes the tools when it is done, whatever ran.

## Per-tool settings

What a tool needs to say about the bundle is said where the bundle is
built, with `toolbundle.Use(tool, opts...)`, and never on the tool. A
struct that embeds a tool to add a method drops the optional
interfaces the tool declared. `toolbundle.Uses(set...)` takes a whole
set with no options.

- `PreApproved()` puts the command in the skill's `allowed-tools` as
  `Bash(<name> <command> *)`. It is the author's choice, per tool, and
  never inferred from the read-only hint, since a hint may make a
  policy stricter and may not alone allow a call.
- `Example(args, caption)` adds a worked command line under the
  command. It is parsed against the command at startup by the parser
  that runs it, so it cannot teach a flag that does not exist. An
  example gives its JSON inline and quotes any word with a character a
  shell treats specially.
- `Stateful()` acknowledges that a tool keeps state across calls.
  `export-skill` warns about a tool that declares a resource, is
  sequential or is a closer, since one call per process loses that
  state; this silences the warning.
- `Hidden()` leaves a tool out of the skill and `help`. It stays
  runnable by name.

## The skill

`SKILL.md` is an `agentskill.Skill`, validated before it is encoded:

- `name` and `description` are the bundle's.
- The body is a title, the description, and `cli`'s usage for every
  command that is not hidden, each followed by its examples.
  [`testdata/skills/file-tools/SKILL.md`](testdata/skills/file-tools/SKILL.md)
  shows one.
- `allowed-tools` lists the pre-approved commands.
- `metadata` holds `version` and `digest`, the sha256 of the body,
  which is exactly what the model reads. No command line carries
  either: a stale skill fails a call with exit 2 and a pointer to
  `help`.

The skill's command lines name the bundle, so the binary must be
installed under `Bundle.Name`; `export-skill` notes when it was run
under another.

## Serving MCP from the same binary

`Bundle.Commands` adds program commands of the author's own. The
nested module `github.com/ChristopherDavenport/toolbundle/mcp` has
one, `mcp`, which serves the bundle's tools over MCP on stdin and
stdout through
[`agenttool/mcpserver`](https://pkg.go.dev/github.com/ChristopherDavenport/agenttool/mcpserver):

```go
import "github.com/ChristopherDavenport/toolbundle/mcp"

os.Exit(toolbundle.Main(context.Background(), toolbundle.Bundle{
	Name:        "file-tools",
	Description: "…",
	Tools:       []toolbundle.Tool{ … },
	Commands:    []toolbundle.Command{mcp.Command()},
}, os.Args))
```

```sh
claude mcp add file-tools -- file-tools mcp   # a host that takes MCP
file-tools export-skill ~/.claude/skills      # a host that does not
```

- It is a module of its own so that the MCP SDK reaches only the
  programs that serve MCP. A bundle without the command carries none
  of it, which matters where a host forbids MCP and reviews what it
  installs.
- It serves the tools the skill shows, named and versioned as the
  bundle. A hidden tool is not served, since MCP has no tool a client
  may call but not list. Questions are elicitations, progress is
  notifications and records are `_meta`, as from any server built on
  `mcpserver`.
- One process serves the whole session, so a tool that keeps state
  between calls keeps it over MCP. `export-skill` still warns about
  such a tool, since the program loses the state.
- It runs until the client closes stdin or the program is interrupted
  or terminated, then the program closes the tools. It takes no
  arguments, and nothing but the protocol goes to stdout.
- The skill does not mention it. The model reaches the command line
  through the skill and the server through the host's MCP
  configuration, and a host uses one or the other.
- `mcp.CommandWith(mcpserver.Options{…})` passes options to the
  server.

## What pre-approval grants in Claude Code

From the [permissions](https://code.claude.com/docs/en/permissions)
and [skills](https://code.claude.com/docs/en/skills) documentation:

- **It is temporary.** A skill's `allowed-tools` lets the listed
  commands run without asking only during the turn that invokes the
  skill. It is a convenience inside one turn, not a standing grant.
- **Chaining does not widen it.** A command line is split on `&&`,
  `||`, `;`, `|`, `|&`, `&` and newlines, and each part must match a
  rule, so `file-tools read_file --path x && rm -rf ~` is asked about.
- **A redirect is checked on its own.** For `> file`, the target is
  checked against the `Edit` rules and the working directories as if
  the model had written the file, so `Bash(file-tools read_file *)`
  allows the command and not where its output goes.
- **Program options break the match.** `file-tools --out /tmp
  read_file …` does not match `Bash(file-tools read_file *)` and is
  asked about. That is the safe direction, and why the skill puts no
  option on every command line.

Tried in Claude Code 2.1.292, with a skill pre-approving
`Bash(file-tools read_file *)`:

| Command | In the turn that uses the skill |
| --- | --- |
| `file-tools read_file --path data.txt` | runs |
| `file-tools read_file '{"path": "data.txt"}'` | runs |
| `file-tools read_file - < args.json` | runs |
| `file-tools read_file - <<'EOF'`, then `not json` | runs: a heredoc is one command, not split at its newlines |
| `file-tools read_file - <<'EOF'`, then `{"path": "data.txt"}` | asked about: "Contains brace with quote character (expansion obfuscation)" |

The heredoc is not split, but a brace followed by a quote in its body
trips a check that asks whatever the rules say, and every JSON object
has one. `$'…'` and `$(…)` are asked about too. Flags and single-quoted
inline JSON are what pre-approval covers, so the usage `cli` renders,
from agenttool v0.0.17, leads with flags: a field of an object is
`--object.field`, a map entry `--map "key=value"`, and a value is
double-quoted when it holds a space, a single quote or a line break.
JSON is left for what no flag can give, and the heredoc for JSON
holding a single quote. Without the skill, all of them are asked
about.

## What does not translate

One call is one process. A tool that holds state across calls, a
persistent shell or a container session, loses it at exit, and its
`Resource` or `Sequential` declaration orders nothing against a call in
another process. Such a tool is better left out of the bundle. A
background process the program talks to would be a tool server under
another name, and where MCP is forbidden it would be a way around the
policy rather than a way to follow it, so the bundle does not build
one.

That is a background process the bundle would start for itself. The
`mcp` command is not one: the host starts it, as it starts any MCP
server it allows, and a host that forbids MCP never does.

Not in this version: an AGENTS.md index, an MCP transport other than
stdio, and a `--json` output envelope.

## License

MIT; see [LICENSE](LICENSE).
