# Changelog

All user-visible changes to this library. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/); before v1.0.0 minor
versions may break the API.

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
