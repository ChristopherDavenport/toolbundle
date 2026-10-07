package toolbundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/agentskill"
	"github.com/ChristopherDavenport/agenttool"
	"github.com/ChristopherDavenport/agenttool/cli"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata")

type readArgs struct {
	Path     string `json:"path" desc:"Path to read, relative to the working tree"`
	MaxBytes int    `json:"max_bytes,omitempty" desc:"Stop after this many bytes"`
}

type searchArgs struct {
	Pattern string   `json:"pattern" desc:"Regular expression to search for"`
	Paths   []string `json:"paths,omitempty" desc:"Directories to search; the working tree when none"`
}

type writeArgs struct {
	Path    string `json:"path" desc:"Path to write"`
	Content string `json:"content" desc:"The file's new content"`
}

// closer counts the times a tool's Close is called.
type closer struct {
	n   int
	err error
}

func (c *closer) close() error { c.n++; return c.err }

func readFile() agenttool.Tool {
	return agenttool.New("read_file", "Read a file in the working tree.",
		func(ctx context.Context, a readArgs) (string, error) { return "read " + a.Path, nil },
		agenttool.WithAnnotations(agenttool.Annotations{ReadOnly: true}))
}

func search() agenttool.Tool {
	return agenttool.New("search", "Search files for a pattern.",
		func(ctx context.Context, a searchArgs) (string, error) {
			return "searched " + strings.Join(a.Paths, ",") + " for " + a.Pattern, nil
		}, agenttool.WithAnnotations(agenttool.Annotations{ReadOnly: true}))
}

func writeFile() agenttool.Tool {
	return agenttool.New("write_file", "Write a file in the working tree.",
		func(ctx context.Context, a writeArgs) (string, error) { return "wrote " + a.Path, nil },
		agenttool.WithAnnotations(agenttool.Annotations{Destructive: true}))
}

func reindex() agenttool.Tool {
	return agenttool.New("reindex", "Rebuild the search index.",
		func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "reindexed", nil })
}

func confirm() agenttool.Tool {
	return agenttool.New("confirm", "Ask before acting.",
		func(ctx context.Context, _ agenttool.NoArgs) (string, error) {
			ask, ok := agenttool.ElicitorFrom(ctx)
			if !ok {
				return "", errors.New("nobody to ask")
			}
			a, err := ask(ctx, agenttool.Elicitation{Message: "Go ahead?"})
			if err != nil {
				return "", err
			}
			return "answered " + string(a.Action), nil
		})
}

// fileTools is the bundle the golden skill is exported from.
func fileTools() Bundle {
	return Bundle{
		Name:        "file-tools",
		Description: "Read, search and write files in the working tree. Use when a task needs the contents of a file or a change to one.",
		Version:     "v1.2.3",
		Tools: []Tool{
			Use(readFile(), PreApproved(),
				Example("read_file --path go.mod", "Read a file"),
				Example("read_file --path README.md --max_bytes 200", "Read the start of a file.")),
			Use(search(), PreApproved(),
				Example(`search --pattern 'func [A-Z]' --paths cmd --paths internal`, "")),
			Use(writeFile(),
				Example(`write_file --path notes.txt '{"content": "one line\n"}'`, "Write a file")),
			Use(reindex(), Hidden()),
		},
	}
}

// serve is a program command that prints what it was given and exits
// with status.
func serve(status int) Command {
	return Command{Name: "serve", Run: func(ctx context.Context, p Program, args []string) int {
		in, _ := io.ReadAll(p.Stdin)
		var names []string
		for _, t := range p.Tools {
			names = append(names, t.Name())
		}
		fmt.Fprintf(p.Stdout, "%s %s %s %q %q\n", p.Name, p.Version, strings.Join(names, ","), args, in)
		fmt.Fprintln(p.Stderr, "serving")
		return status
	}}
}

type result struct {
	status         int
	stdout, stderr string
}

func run(t *testing.T, b Bundle, stdin string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := b.main(context.Background(), append([]string{b.Name}, args...), strings.NewReader(stdin), &stdout, &stderr)
	return result{status, stdout.String(), stderr.String()}
}

func TestExportSkill(t *testing.T) {
	dir := t.TempDir()
	r := run(t, fileTools(), "", "export-skill", dir)
	if r.status != cli.ExitOK {
		t.Fatalf("status %d, stderr:\n%s", r.status, r.stderr)
	}
	path := filepath.Join(dir, "file-tools", "SKILL.md")
	wantOut := "wrote " + path + "\n" +
		"pre-approved in allowed-tools, run without asking in the turn that uses the skill:\n" +
		"  Bash(file-tools read_file *)\n" +
		"  Bash(file-tools search *)\n"
	if r.stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, wantOut)
	}
	if r.stderr != "" {
		t.Errorf("stderr: %s", r.stderr)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "skills", "file-tools", "SKILL.md")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("SKILL.md differs from %s; run go test -update and review the diff\ngot:\n%s", golden, got)
	}

	s, err := agentskill.LoadDir(filepath.Join(dir, "file-tools"))
	if err != nil {
		t.Fatal(err)
	}
	if problems := s.Validate(); problems != nil {
		t.Errorf("problems: %v", problems)
	}
	rules, err := s.Rules()
	if err != nil || len(rules) != 2 {
		t.Errorf("rules = %v, %v; want two", rules, err)
	}
	sum := sha256.Sum256([]byte(s.Body))
	if d := s.Metadata["digest"]; d != hex.EncodeToString(sum[:]) {
		t.Errorf("digest %s is not the sha256 of the body", d)
	}
	if v := s.Metadata["version"]; v != "v1.2.3" {
		t.Errorf("version = %q", v)
	}
	if strings.Contains(s.Body, "reindex") {
		t.Error("the hidden tool is in the skill")
	}
}

func TestExportSkillRefusesToReplace(t *testing.T) {
	dir := t.TempDir()
	if r := run(t, fileTools(), "", "export-skill", dir); r.status != cli.ExitOK {
		t.Fatalf("first export: %d %s", r.status, r.stderr)
	}
	path := filepath.Join(dir, "file-tools", "SKILL.md")
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(t, fileTools(), "", "export-skill", dir)
	if r.status != cli.ExitFailed || !strings.Contains(r.stderr, "--force") {
		t.Errorf("second export: %d %q", r.status, r.stderr)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Error("the existing SKILL.md was replaced")
	}
	if r := run(t, fileTools(), "", "export-skill", dir, "--force"); r.status != cli.ExitOK {
		t.Fatalf("--force: %d %s", r.status, r.stderr)
	}
	if data, _ := os.ReadFile(path); string(data) == "mine" {
		t.Error("--force did not replace the SKILL.md")
	}
}

func TestExportSkillUsage(t *testing.T) {
	for _, args := range [][]string{
		{"export-skill"},
		{"export-skill", "a", "b"},
		{"export-skill", "--frce", "a"},
	} {
		if r := run(t, fileTools(), "", args...); r.status != cli.ExitUsage {
			t.Errorf("%q: status %d", args, r.status)
		}
	}
}

func TestExportSkillWarns(t *testing.T) {
	shell := func(opts ...agenttool.Option) agenttool.Tool {
		return agenttool.New("shell", "Run a command in a persistent shell.",
			func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "", nil }, opts...)
	}
	tests := []struct {
		name string
		tool Tool
		want string
	}{
		{"resource", Use(shell(agenttool.WithResource("shell:session"))), `shell declares the resource "shell:session"`},
		{"sequential", Use(shell(agenttool.WithSequential())), "shell is sequential"},
		{"closer", Use(shell(agenttool.WithCloser(func() error { return nil }))), "shell owns something it closes"},
		{"both", Use(shell(agenttool.WithSequential(), agenttool.WithCloser(func() error { return nil }))), "shell is sequential and owns something it closes"},
		{"stateful", Use(shell(agenttool.WithSequential()), Stateful()), ""},
		{"hidden still warns", Use(shell(agenttool.WithSequential()), Hidden()), "shell is sequential"},
		{"plain", Use(shell()), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Bundle{Name: "shell-tools", Description: "A shell.", Tools: []Tool{tt.tool}}
			r := run(t, b, "", "export-skill", t.TempDir())
			if r.status != cli.ExitOK {
				t.Fatalf("status %d: %s", r.status, r.stderr)
			}
			if tt.want == "" {
				if strings.Contains(r.stderr, "warning") {
					t.Errorf("unexpected warning: %s", r.stderr)
				}
				return
			}
			if !strings.Contains(r.stderr, "warning: "+tt.want+",") {
				t.Errorf("stderr %q does not warn %q", r.stderr, tt.want)
			}
		})
	}
}

func TestExportSkillNotesTheBinaryName(t *testing.T) {
	b := fileTools()
	var stdout, stderr bytes.Buffer
	status := b.main(context.Background(), []string{"/tmp/go-build123/exe/main", "export-skill", t.TempDir()}, strings.NewReader(""), &stdout, &stderr)
	if status != cli.ExitOK {
		t.Fatalf("status %d: %s", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "run as main, and the skill calls it file-tools") {
		t.Errorf("stderr: %q", stderr.String())
	}
}

func TestStartupRefuses(t *testing.T) {
	tool := func(name string) agenttool.Tool {
		return agenttool.New(name, "A tool.", func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "", nil })
	}
	withExample := func(ex string) []Tool {
		return []Tool{Use(readFile(), Example(ex, ""))}
	}
	tests := []struct {
		name   string
		bundle Bundle
		want   string
	}{
		{"no name", Bundle{Description: "d"}, "no name"},
		{"upper case name", Bundle{Name: "File-Tools", Description: "d"}, "lowercase"},
		{"doubled hyphen", Bundle{Name: "file--tools", Description: "d"}, "consecutive hyphens"},
		{"long name", Bundle{Name: strings.Repeat("a", 65), Description: "d"}, "64"},
		{"no description", Bundle{Name: "file-tools"}, "description"},
		{"zero tool", Bundle{Name: "t", Description: "d", Tools: []Tool{{}}}, "tool 0 is nil"},
		{"tool named version", Bundle{Name: "t", Description: "d", Tools: []Tool{Use(tool("version"))}}, `"version"`},
		{"tool named export-skill", Bundle{Name: "t", Description: "d", Tools: []Tool{Use(tool("export-skill"))}}, `"export-skill"`},
		{"tool named help", Bundle{Name: "t", Description: "d", Tools: []Tool{Use(tool("help"))}}, `"help"`},
		{"tool named schema", Bundle{Name: "t", Description: "d", Tools: []Tool{Use(tool("schema"))}}, `"schema"`},
		{"two tools of one name", Bundle{Name: "t", Description: "d", Tools: []Tool{Use(tool("a")), Use(tool("a"), Hidden())}}, `"a"`},
		{"example of an unknown flag", Bundle{Name: "t", Description: "d", Tools: withExample("read_file --file x")}, "file"},
		{"example of a wrong type", Bundle{Name: "t", Description: "d", Tools: withExample("read_file --path x --max_bytes ten")}, "max_bytes"},
		{"example of another command", Bundle{Name: "t", Description: "d", Tools: withExample("search --pattern x")}, "must start with the command, read_file"},
		{"example with program options", Bundle{Name: "t", Description: "d", Tools: withExample("--record r read_file --path x")}, "must start with the command"},
		{"example reading stdin", Bundle{Name: "t", Description: "d", Tools: withExample("read_file -")}, "inline"},
		{"example with a redirection", Bundle{Name: "t", Description: "d", Tools: withExample("read_file --path x > y")}, `'>' must be quoted`},
		{"example asking for help", Bundle{Name: "t", Description: "d", Tools: withExample("read_file --help")}, "not a call"},
		{"example with bad JSON", Bundle{Name: "t", Description: "d", Tools: withExample("read_file '{path}'")}, "JSON"},
		{"command with no name", Bundle{Name: "t", Description: "d", Commands: []Command{{Run: serve(0).Run}}}, "command 0"},
		{"command named as an option", Bundle{Name: "t", Description: "d", Commands: []Command{{Name: "--mcp", Run: serve(0).Run}}}, `"--mcp"`},
		{"command with no Run", Bundle{Name: "t", Description: "d", Commands: []Command{{Name: "serve"}}}, "no Run"},
		{"command named version", Bundle{Name: "t", Description: "d", Commands: []Command{{Name: "version", Run: serve(0).Run}}}, `"version"`},
		{"command named help", Bundle{Name: "t", Description: "d", Commands: []Command{{Name: "help", Run: serve(0).Run}}}, `"help"`},
		{"two commands of one name", Bundle{Name: "t", Description: "d", Commands: []Command{serve(0), serve(1)}}, `two commands are named "serve"`},
		{"tool named as a command", Bundle{Name: "t", Description: "d", Tools: []Tool{Use(tool("serve"), Hidden())}, Commands: []Command{serve(0)}}, `a tool is named "serve"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, args := range [][]string{{"version"}, {"export-skill", t.TempDir()}, {"help"}} {
				r := run(t, tt.bundle, "", args...)
				if r.status != cli.ExitUsage {
					t.Fatalf("%q: status %d, stderr %q", args, r.status, r.stderr)
				}
				if !strings.Contains(r.stderr, tt.want) {
					t.Errorf("%q: stderr %q does not mention %q", args, r.stderr, tt.want)
				}
				if r.stdout != "" {
					t.Errorf("%q: stdout %q", args, r.stdout)
				}
			}
		})
	}
}

func TestRun(t *testing.T) {
	b := fileTools()
	b.Tools = append(b.Tools, Use(confirm()))
	tests := []struct {
		name   string
		stdin  string
		args   []string
		status int
		stdout string // a substring of stdout
		stderr string // a substring of stderr
	}{
		{name: "flags", args: []string{"read_file", "--path", "go.mod"}, stdout: "read go.mod"},
		{name: "repeated flags", args: []string{"search", "--pattern", "x", "--paths", "a", "--paths", "b"}, stdout: "searched a,b for x"},
		{name: "json on stdin", stdin: `{"path": "go.mod"}`, args: []string{"read_file", "-"}, stdout: "read go.mod"},
		{name: "program options before the command", args: []string{"--out", t.TempDir(), "read_file", "--path", "x"}, stdout: "read x"},
		{name: "tool error", args: []string{"read_file"}, status: cli.ExitFailed, stderr: "path"},
		{name: "unknown command", args: []string{"nope"}, status: cli.ExitUsage, stderr: `unknown command "nope"`},
		{name: "version", args: []string{"version"}, stdout: "file-tools v1.2.3\n"},
		{name: "version takes nothing", args: []string{"version", "x"}, status: cli.ExitUsage},
		{name: "program commands are first only", args: []string{"--out", "x", "version"}, status: cli.ExitUsage, stderr: `unknown command "version"`},
		{name: "hidden tool runs", args: []string{"reindex"}, stdout: "reindexed"},
		{name: "hidden tool runs after options", args: []string{"--answer", "accept", "--record", filepath.Join(t.TempDir(), "r"), "reindex"}, stdout: "reindexed"},
		{name: "help leaves the hidden tool out", args: []string{"help"}, stdout: "`write_file`"},
		{name: "hidden tool has no schema", args: []string{"schema", "reindex"}, status: cli.ExitUsage},
		{name: "question for a model", args: []string{"confirm"}, status: cli.ExitNeedsAnswer, stdout: `"message":"Go ahead?"`},
		{name: "question answered", args: []string{"--answer", "accept", "confirm"}, stdout: "answered accept"},
		{name: "question for a person", stdin: "y\n", args: []string{"--ask", "confirm"}, stdout: "answered accept", stderr: "Go ahead?"},
		{name: "--ask before options", stdin: "n\n", args: []string{"--ask", "--out", t.TempDir(), "confirm"}, stdout: "answered decline"},
		{name: "--ask is first only", args: []string{"read_file", "--ask"}, status: cli.ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, b, tt.stdin, tt.args...)
			if r.status != tt.status {
				t.Fatalf("status %d, want %d\nstdout: %s\nstderr: %s", r.status, tt.status, r.stdout, r.stderr)
			}
			if !strings.Contains(r.stdout, tt.stdout) {
				t.Errorf("stdout %q does not hold %q", r.stdout, tt.stdout)
			}
			if !strings.Contains(r.stderr, tt.stderr) {
				t.Errorf("stderr %q does not hold %q", r.stderr, tt.stderr)
			}
		})
	}

	if r := run(t, b, "", "help"); strings.Contains(r.stdout, "reindex") {
		t.Errorf("help lists the hidden tool:\n%s", r.stdout)
	}
}

func TestRunCommand(t *testing.T) {
	b := fileTools()
	b.Commands = []Command{serve(cli.ExitOK), {Name: "fail", Run: serve(cli.ExitFailed).Run}}
	tests := []struct {
		name   string
		stdin  string
		args   []string
		status int
		stdout string
		stderr string
	}{
		{name: "a command", stdin: "in", args: []string{"serve", "--port", "1"}, stdout: `file-tools v1.2.3 read_file,search,write_file ["--port" "1"] "in"` + "\n", stderr: "serving\n"},
		{name: "its status", args: []string{"fail"}, status: cli.ExitFailed, stdout: "file-tools v1.2.3", stderr: "serving\n"},
		{name: "first only", args: []string{"--out", "x", "serve"}, status: cli.ExitUsage, stderr: `unknown command "serve"`},
		{name: "not a tool", args: []string{"schema", "serve"}, status: cli.ExitUsage},
		{name: "tools still run", args: []string{"read_file", "--path", "go.mod"}, stdout: "read go.mod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, b, tt.stdin, tt.args...)
			if r.status != tt.status {
				t.Fatalf("status %d, want %d\nstdout: %s\nstderr: %s", r.status, tt.status, r.stdout, r.stderr)
			}
			if !strings.Contains(r.stdout, tt.stdout) {
				t.Errorf("stdout %q does not hold %q", r.stdout, tt.stdout)
			}
			if !strings.Contains(r.stderr, tt.stderr) {
				t.Errorf("stderr %q does not hold %q", r.stderr, tt.stderr)
			}
		})
	}

	if r := run(t, b, "", "help"); strings.Contains(r.stdout, "serve") {
		t.Errorf("help lists the command:\n%s", r.stdout)
	}
	dir := t.TempDir()
	if r := run(t, b, "", "export-skill", dir); r.status != cli.ExitOK {
		t.Fatalf("export-skill: %d %s", r.status, r.stderr)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "file-tools", "SKILL.md")); strings.Contains(string(data), "serve") {
		t.Error("the skill names the command")
	}
}

func TestMainClosesTheTools(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		err    error
		status int
	}{
		{"a call", []string{"owner"}, nil, cli.ExitOK},
		{"a failed call", []string{"owner", "--bad"}, nil, cli.ExitUsage},
		{"export-skill", []string{"export-skill", "DIR"}, nil, cli.ExitOK},
		{"version", []string{"version"}, nil, cli.ExitOK},
		{"a command", []string{"serve"}, nil, cli.ExitOK},
		{"a failed close leaves the status", []string{"owner"}, errors.New("stuck"), cli.ExitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &closer{err: tt.err}
			owner := agenttool.New("owner", "Owns something.",
				func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "ok", nil },
				agenttool.WithCloser(c.close))
			hidden := &closer{}
			maint := agenttool.New("maint", "Maintenance.",
				func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "ok", nil },
				agenttool.WithCloser(hidden.close))
			b := Bundle{Name: "owner", Description: "Owns.", Tools: []Tool{Use(owner, Stateful()), Use(maint, Hidden(), Stateful())}, Commands: []Command{serve(cli.ExitOK)}}
			args := append([]string(nil), tt.args...)
			for i, a := range args {
				if a == "DIR" {
					args[i] = t.TempDir()
				}
			}
			r := run(t, b, "", args...)
			if r.status != tt.status {
				t.Fatalf("status %d, want %d: %s", r.status, tt.status, r.stderr)
			}
			if c.n != 1 || hidden.n != 1 {
				t.Errorf("closed %d and %d times, want once each", c.n, hidden.n)
			}
			if tt.err != nil && !strings.Contains(r.stderr, `closing the tools: agenttool: close "owner": stuck`) {
				t.Errorf("stderr %q does not report the close", r.stderr)
			}
		})
	}

	t.Run("a bundle refused at startup", func(t *testing.T) {
		c := &closer{}
		owner := agenttool.New("version", "Owns something.",
			func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "ok", nil },
			agenttool.WithCloser(c.close))
		r := run(t, Bundle{Name: "owner", Description: "Owns.", Tools: []Tool{Use(owner)}}, "", "help")
		if r.status != cli.ExitUsage || c.n != 1 {
			t.Errorf("status %d, closed %d times", r.status, c.n)
		}
	})
}

func TestUses(t *testing.T) {
	set := agenttool.Set{readFile(), search()}
	b := Bundle{Name: "file-tools", Description: "Files.", Tools: Uses(set...)}
	if r := run(t, b, "", "search", "--pattern", "x"); r.status != cli.ExitOK {
		t.Errorf("status %d: %s", r.status, r.stderr)
	}
	if rules := b.allowed(); rules != nil {
		t.Errorf("Uses pre-approved %v", rules)
	}
}

func TestVersionFromBuildInfo(t *testing.T) {
	b := fileTools()
	b.Version = ""
	r := run(t, b, "", "version")
	if r.status != cli.ExitOK || !strings.HasPrefix(r.stdout, "file-tools ") {
		t.Errorf("status %d, stdout %q", r.status, r.stdout)
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		err  string
	}{
		{in: "read_file --path go.mod", want: []string{"read_file", "--path", "go.mod"}},
		{in: "  a\tb  ", want: []string{"a", "b"}},
		{in: `a 'b c' "d e"`, want: []string{"a", "b c", "d e"}},
		{in: `a 'it''s'`, want: []string{"a", "its"}},
		{in: `a "x\"y\\z\$"`, want: []string{"a", `x"y\z$`}},
		{in: `a "x\ny"`, want: []string{"a", `x\ny`}},
		{in: `a '{"k": "v\n"}'`, want: []string{"a", `{"k": "v\n"}`}},
		{in: "a --k=v --n=-1 x@y:z,w+%", want: []string{"a", "--k=v", "--n=-1", "x@y:z,w+%"}},
		{in: "a ''", want: []string{"a", ""}},
		{in: "", want: nil},
		{in: "a 'b", err: "unterminated single quote"},
		{in: `a "b`, err: "unterminated double quote"},
		{in: `a "$HOME"`, err: "use single quotes"},
		{in: "a `x`", err: "must be quoted"},
		{in: "a > b", err: `'>' must be quoted`},
		{in: "a | b", err: `'|' must be quoted`},
		{in: "a; b", err: `';' must be quoted`},
		{in: "a && b", err: `'&' must be quoted`},
		{in: "a *.go", err: `'*' must be quoted`},
		{in: "a ~/x", err: `'~' must be quoted`},
		{in: "a $x", err: `'$' must be quoted`},
		{in: "a =ls", err: `'=' must be quoted`},
		{in: "a\nb", err: `'\n' must be quoted`},
		{in: `a \x`, err: `'\\' must be quoted`},
	}
	for _, tt := range tests {
		got, err := splitWords(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%q: error %v, want %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") || len(got) != len(tt.want) {
			t.Errorf("%q = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestInterop runs the reference skills-ref validator over the golden
// skill, which is what export-skill writes. It needs uvx and the
// network, so it runs only under SKILLS_INTEROP=1 (make interop).
func TestInterop(t *testing.T) {
	if os.Getenv("SKILLS_INTEROP") == "" {
		t.Skip("set SKILLS_INTEROP=1 to run against the skills-ref CLI")
	}
	if _, err := exec.LookPath("uvx"); err != nil {
		t.Skip("uvx not found")
	}
	dir := filepath.Join("testdata", "skills", "file-tools")
	out, err := exec.Command("uvx", "--from", "skills-ref", "agentskills", "validate", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("skills-ref validate: %v\n%s", err, out)
	}
}
