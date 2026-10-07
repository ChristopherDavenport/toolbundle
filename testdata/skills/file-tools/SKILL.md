---
name: file-tools
description: Read, search and write files in the working tree. Use when a task needs the contents of a file or a change to one.
allowed-tools: Bash(file-tools read_file *) Bash(file-tools search *)
metadata:
  digest: 48e039a6c399a63d481d5b66c1ac53a727158b33f2372868495ce5ed8458e3ac
  version: v1.2.3
---

# file-tools

Read, search and write files in the working tree. Use when a task needs the contents of a file or a change to one.

## Calling `file-tools`

Run one command per call. A command takes its arguments as flags, as one JSON object, or both. Use flags wherever the command has them:

```sh
file-tools <command> --name "it's two words" --object.field value --map "key=O'Brien"
```

A field of an object is a flag named by its path, `--object.field`, and an entry of a map is `--map key=value`, given once per entry. Put a value in double quotes whenever it holds a space, a single quote or a line break, a map entry's included, and escape `\"`, `\$`, `` \` `` and `\\` inside them. Type a line break as a line break inside the quotes: `\n` there is a backslash and an n. A boolean flag stands alone for true, or takes `=false`.

A value no flag can give, such as an array of objects, goes in a JSON argument after the flags, which adds to them: each value is given once, as a flag or in the JSON. Give it in single quotes on one line, writing a newline inside a string as `\n`:

```sh
file-tools <command> --name value '{"items": [{"name": "value"}]}'
```

Only when the JSON holds a single quote, which would end the quoting, give it on stdin in a quoted heredoc instead:

```sh
file-tools <command> - <<'EOF'
{"items": [{"name": "it's"}]}
EOF
```

The exit status says what happened:

- 0: the command succeeded, and its output is on stdout. If stderr says the output could not be written, the command still ran: do not run it again for that.
- 1: the tool failed or refused the arguments, and says why on stderr. Correct the call and run it again.
- 2: the command did not run, because the command line was not understood or the program could not start it, and stderr says why.
- 3: the tool asked the user a question that nobody answered, and was told it was cancelled. Stdout holds the question as JSON, with what the tool returned. Ask the user, then run the same command again with `--answer` before the command name: `--answer accept`, `--answer decline`, or `--answer '{...}'` with the fields a form asks for. A command that asks again needs every earlier answer again, in the order given.

A file a command produces, such as an image, is written to disk and its path printed. `file-tools help <command>` prints one command's usage and `file-tools schema <command>` its JSON Schema.

## Commands

### `read_file`

Read a file in the working tree.

Hints from the tool: read-only.

```sh
file-tools read_file --path <string> [--max_bytes <integer>]
```

- `--path` (string, required): Path to read, relative to the working tree
- `--max_bytes` (integer): Stop after this many bytes

Read a file:

```sh
file-tools read_file --path go.mod
```

Read the start of a file:

```sh
file-tools read_file --path README.md --max_bytes 200
```

### `search`

Search files for a pattern.

Hints from the tool: read-only.

```sh
file-tools search --pattern <string> [--paths <string> ...]
```

- `--pattern` (string, required): Regular expression to search for
- `--paths` (array of string, repeatable): Directories to search; the working tree when none

Example:

```sh
file-tools search --pattern 'func [A-Z]' --paths cmd --paths internal
```

### `write_file`

Write a file in the working tree.

Hints from the tool: may be destructive.

```sh
file-tools write_file --path <string> --content <string>
```

- `--path` (string, required): Path to write
- `--content` (string, required): The file's new content

Write a file:

```sh
file-tools write_file --path notes.txt '{"content": "one line\n"}'
```
