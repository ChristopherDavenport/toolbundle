# toolbundle

A set of `agenttool` tools as one command-line program that runs them
and writes its own Agent Skill, for a host that allows no MCP server
but gives the model a shell. The design is the `toolbundle` section of
[agenttool#71](https://github.com/ChristopherDavenport/agenttool/issues/71);
read it before changing behaviour, and record a departure from it in
the changelog and the pull request.

## Modules

| Path  | Module path            | What it is |
| ----- | ---------------------- | ---------- |
| `.`   | `…/toolbundle`         | the program, the skill and `Command` |
| `mcp` | `…/toolbundle/mcp`     | the `mcp` command, over `agenttool/mcpserver` |

- Go 1.25 is the floor. The root package name is `toolbundle`.
- The root depends on `github.com/ChristopherDavenport/agenttool`
  (the contract and its `cli` package),
  `github.com/ChristopherDavenport/agentskill` and the standard library.
  Nothing else; `make deps` enforces it. openresponses and the YAML
  parser come in through those two and are allowed only that way.
- The MCP SDK is never a dependency of the root. It comes in only
  through `mcp`, which requires `agenttool/mcpserver`, so a bundle
  that does not serve MCP carries none of it. Anything else that needs
  the SDK goes in `mcp` or a nested module of its own.
- Both modules are released at one version from one commit, and `mcp`
  requires the root at exactly that version with a `replace` to `..`.
  The rules, and why the replace is load-bearing, are agenttool's: read
  the Releases section of `../agenttool/AGENTS.md`. The scripts under
  `scripts/` are its, pointed at this module. The workspace rule
  against committing a `replace` is about siblings; this one stays
  inside the repository.
- A bare `go test ./...` does not cross into `mcp`. Use the Makefile
  targets.

## Siblings

Peer repositories, each independently versioned:

- `../agenttool`: the tool contract, and `cli`, which turns a `Set`
  into commands, parses and runs them, and renders their usage. This
  module translates nothing itself: what a command line means is
  `cli`'s, so a change to parsing or usage belongs there.
- `../agentskill`: the skill format. This module builds an
  `agentskill.Skill` and writes it with `Encode` after `Validate`; it
  never writes frontmatter by hand.
- `../agenttool/mcpserver`: serves a `Set` over MCP. `mcp` builds the
  server with it and adds nothing to how a tool is served; a change to
  that belongs there.
- `../examples`: a runnable example serving one `Set` in process, over
  MCP and as a bundle is step 4 of the plan.

## Conventions

Mirror `../agenttool`, which also has nested modules: a `Makefile`
with `build`, `deps`, `replaces`, `test`, `vet`, `fmt`, `tidy`,
`tidy-check`, `lint`, `vuln`, `check`, `extracted`, `interop` and
`release` targets, each running over every module, the same CI
workflow shape (minimum and stable Go, lint, tidy-check, extracted), a
`CHANGELOG.md` in Keep a Changelog form, and annotated `v*` and
`mcp/v*` tags whose message becomes the release notes. `make check`
must pass before any commit. `make extracted` builds `mcp` against the
root its `go.mod` requires, from the proxy; it skips it between
releases once the root has moved on, and needs the network. `make
interop` runs the `skills-ref` validator over the golden skill; it
needs `uvx` and the network.

Tests are table-driven and offline. The skill `export-skill` writes is
golden at `testdata/skills/file-tools/SKILL.md`; regenerate it with
`go test . -update` and review the diff, since it is what a model
reads.

The user's Go environment sets `-mod=mod`; run targets as
`GOWORK=off GOFLAGS=-mod=readonly make check`.
