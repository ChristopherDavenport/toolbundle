# Changelog

All user-visible changes to this library. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/); before v1.0.0 minor
versions may break the API.

## Unreleased

- Added: `Bundle.Commands`, program commands of the author's own,
  recognised only as the first argument beside `export-skill` and
  `version` and left out of the skill. A `Command` runs with a
  `Program`, the bundle's name, version, visible tools and standard
  streams, after the startup checks and before the tools are closed.
  A command named as one of the program's own, as a tool or as another
  command is refused at startup.
- Added: the nested module `github.com/ChristopherDavenport/toolbundle/mcp`,
  whose `mcp.Command()` serves the bundle's visible tools over MCP on
  stdio through `agenttool/mcpserver`, so one binary is both the
  program and the server: `<name> mcp`. It is released with the root
  as `mcp/vX.Y.Z`, and the MCP SDK reaches only the programs that use
  it.
- Departs from
  [agenttool#71](https://github.com/ChristopherDavenport/agenttool/issues/71),
  which left an MCP mode out of v1 and spelt it `--mcp-stdio`: it is a
  program command, `mcp`, like `export-skill` and `version`, and it
  lives in a nested module rather than the root, so the root still
  never depends on the MCP SDK.
- Dependencies: `mcp` requires agenttool/mcpserver v0.0.18 and the MCP
  Go SDK v1.8.0; the root's are unchanged.

## v0.0.1 - 2026-10-07

- Added: `Main` runs a `Bundle` of agenttool tools as one program
  through `agenttool/cli`, with `export-skill <dir>`, which writes the
  Agent Skill that teaches a model to call it, `version`, and `--ask`
  for a person at the terminal. `Use` and `Uses` put tools in a bundle,
  with the options `PreApproved`, `Example`, `Stateful` and `Hidden`.
  The bundle's name, its skill, its tool names and its examples are
  checked at startup. This is the `toolbundle` part of
  [agenttool#71](https://github.com/ChristopherDavenport/agenttool/issues/71).
- Dependencies: agenttool v0.0.17, whose usage leads with flags and
  gives object fields and maps flags of their own, and agentskill
  v0.0.11.
