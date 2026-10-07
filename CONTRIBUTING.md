# Contributing

Issues and pull requests are welcome.

## Before you start

This module is the program around `agenttool/cli` and the skill it
writes. What a command line means, how a call runs and the usage text
are `cli`'s, and the skill format is `agentskill`'s; a change to either
belongs in that repository. The design is the `toolbundle` section of
[agenttool#71](https://github.com/ChristopherDavenport/agenttool/issues/71);
read it first.

For anything larger than a bug fix, open an issue first so the shape of
the change can be discussed before you spend time on it.

## Development

Go 1.25 or later is required. The full local check is:

```sh
make check        # gofmt, tidy, vet, deps, staticcheck, govulncheck, race tests
```

The module depends on `agenttool`, `agentskill` and the standard
library; `make deps` fails if anything else creeps in.

The skill `export-skill` writes is golden at
`testdata/skills/file-tools/SKILL.md`. Regenerate it with
`go test . -update` and review the diff: it is what a model reads.
The reference validator is run over it by:

```sh
make interop      # needs uvx and the network
```

## Releases

This repository is a single module, tagged `vX.Y.Z`. With the
changelog's *Unreleased* section written:

```sh
make release VERSION=v0.1.0
```

dates the changelog, runs `make tidy` and `make check`, commits, guards
and tags the version with the changelog section as the message, and
pushes the branch and the tag in one atomic push. The release workflow
publishes a GitHub release from the tag message, and the Go module
proxy picks the version up. Before v1.0.0 the API may change between
minor versions; the changelog records every break.

`make release-guard TAG=<tag>` is what stands between a mistake and a
permanent one, and `make release` runs it before the tag it writes. It
refuses a dirty tree, a tag that already exists locally or on origin,
and a version that does not sort above the current release — the one
mistake nothing can undo, since the proxy and the checksum database
keep every published version forever. It runs after the release commit
and before the tag, while everything is still local, so a refusal costs
a `git reset --hard HEAD~1`.

The sibling repositories with nested modules carry a longer
`release-guard.sh` that also holds a `<dir>/vX.Y.Z` tag to its module's
`go.mod`. If a nested module is ever added here, that is the part to
bring over.

## Pull requests

- Keep the change focused; unrelated cleanups belong in their own PR.
- Add or update tests. Tests are table-driven and run offline.
- Run `make check` before pushing. CI runs the same steps on the minimum
  and current Go versions.
- Note user-visible changes under *Unreleased* in `CHANGELOG.md`.
