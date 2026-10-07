# toolbundle

A set of `agenttool` tools as one command-line program that runs them
and writes its own Agent Skill, for a host that allows no MCP server
but gives the model a shell. The design is the `toolbundle` section of
[agenttool#71](https://github.com/ChristopherDavenport/agenttool/issues/71);
read it before changing behaviour, and record a departure from it in
the changelog and the pull request.

## Module

- Module path: `github.com/ChristopherDavenport/toolbundle`.
- Go 1.25 is the floor. The root package name is `toolbundle`.
- One module. It depends on `github.com/ChristopherDavenport/agenttool`
  (the contract and its `cli` package),
  `github.com/ChristopherDavenport/agentskill` and the standard library.
  Nothing else; `make deps` enforces it. openresponses and the YAML
  parser come in through those two and are allowed only that way.
- The MCP SDK is never a dependency. An MCP mode would bring it in and
  is out of v1.

## Siblings

Peer repositories, each independently versioned:

- `../agenttool`: the tool contract, and `cli`, which turns a `Set`
  into commands, parses and runs them, and renders their usage. This
  module translates nothing itself: what a command line means is
  `cli`'s, so a change to parsing or usage belongs there.
- `../agentskill`: the skill format. This module builds an
  `agentskill.Skill` and writes it with `Encode` after `Validate`; it
  never writes frontmatter by hand.
- `../examples`: a runnable example serving one `Set` in process, over
  MCP and as a bundle is step 4 of the plan.

## Conventions

Mirror `../agentskill`: a `Makefile` with `build`, `deps`, `test`,
`vet`, `fmt`, `tidy`, `tidy-check`, `lint`, `vuln`, `check`, `interop`
and `release` targets, the same CI workflow shape (minimum and stable
Go, lint, tidy-check), a `CHANGELOG.md` in Keep a Changelog form, and
annotated `v*` tags whose message becomes the release notes. `make
check` must pass before any commit. `make interop` runs the
`skills-ref` validator over the golden skill; it needs `uvx` and the
network.

Tests are table-driven and offline. The skill `export-skill` writes is
golden at `testdata/skills/file-tools/SKILL.md`; regenerate it with
`go test . -update` and review the diff, since it is what a model
reads.

The user's Go environment sets `-mod=mod`; run targets as
`GOWORK=off GOFLAGS=-mod=readonly make check`.
