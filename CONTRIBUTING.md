# Contributing

Issues and pull requests are welcome.

## Before you start

This module is the program around `agenttool/cli` and the skill it
writes, and its nested module `mcp` serves the same tools over MCP
through `agenttool/mcpserver`. What a command line means, how a call
runs and the usage text are `cli`'s, how a tool is served over MCP is
`mcpserver`'s, and the skill format is `agentskill`'s; a change to any
of them belongs in that repository. The design is the `toolbundle` section of
[agenttool#71](https://github.com/ChristopherDavenport/agenttool/issues/71);
read it first.

For anything larger than a bug fix, open an issue first so the shape of
the change can be discussed before you spend time on it.

## Development

Go 1.25 or later is required. The full local check is:

```sh
make check        # gofmt, tidy, vet, deps, replaces, staticcheck, govulncheck, race tests
make extracted    # build, vet and test mcp as a consumer gets it; needs the network
```

Every target runs over both modules; a bare `go test ./...` does not
reach `mcp`. The root module depends on `agenttool`, `agentskill` and
the standard library; `make deps` fails if anything else creeps in.
The MCP SDK comes in only through `mcp`.

The skill `export-skill` writes is golden at
`testdata/skills/file-tools/SKILL.md`. Regenerate it with
`go test . -update` and review the diff: it is what a model reads.
The reference validator is run over it by:

```sh
make interop      # needs uvx and the network
```

## Releases

This repository holds two modules, the root tagged `vX.Y.Z` and `mcp`
tagged `mcp/vX.Y.Z`, released at one version from one commit. `mcp`
requires the root at exactly that version, with a `replace` to `..` so
the release commit can name a version the proxy does not serve yet.
With the changelog's *Unreleased* section written:

```sh
make release VERSION=v0.1.0
```

points `mcp`'s require of the root at the version, dates the
changelog, runs `make tidy` and `make check`, checks the require
landed, commits, guards and tags the root and then `mcp` with the
changelog section as the message, and pushes the branch and both tags
in one atomic push. The release workflow
publishes a GitHub release from the tag message, and the Go module
proxy picks the version up. Before v1.0.0 the API may change between
minor versions; the changelog records every break.

`make release-guard TAG=<tag>` is what stands between a mistake and a
permanent one, and `make release` runs it before each tag it writes. It
refuses a dirty tree, a tag that already exists locally or on origin,
and a version that does not sort above the current release — the one
mistake nothing can undo, since the proxy and the checksum database
keep every published version forever. For `mcp/vX.Y.Z` it also refuses
a `go.mod` that does not require the root at `vX.Y.Z`, and a root tag
of that version that does not name this commit. It runs after the
release commit and before the tags, while everything is still local,
so a refusal costs a `git reset --hard HEAD~1` and a `git tag -d`.

The scripts are agenttool's, pointed at this module; its `AGENTS.md`
explains each rule.

## Pull requests

- Keep the change focused; unrelated cleanups belong in their own PR.
- Add or update tests. Tests are table-driven and run offline.
- Run `make check` before pushing. CI runs the same steps on the minimum
  and current Go versions.
- Note user-visible changes under *Unreleased* in `CHANGELOG.md`.
