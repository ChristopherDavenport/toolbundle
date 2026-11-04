package toolbundle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/agenttool"
	"github.com/ChristopherDavenport/agenttool/cli"
)

type noteArgs struct {
	Name string `json:"name" desc:"The note's name"`
}

// noteTool is a note tool of the three-ways example: its facts claim is
// the note file a call touches, as a call of the host's read or write
// tool on that path.
func noteTool(name, verb string, opts ...agenttool.Option) agenttool.Tool {
	opts = append(opts, agenttool.WithFacts(func(_ context.Context, args json.RawMessage) (agenttool.Facts, error) {
		a, err := agenttool.Decode[noteArgs](args)
		if err != nil {
			return agenttool.Facts{}, err
		}
		path := ".notes/" + a.Name + ".json"
		target, _ := json.Marshal(map[string]string{"path": path})
		return agenttool.Facts{Calls: []agenttool.FactCall{{Tool: verb, Args: target, Text: verb + " " + path}}}, nil
	}))
	return agenttool.New(name, "A note tool.",
		func(ctx context.Context, a noteArgs) (string, error) { return verb + " " + a.Name, nil }, opts...)
}

// notesBundle is the three-ways notes program: two claiming tools, one
// of them hidden, and one that makes no claim.
func notesBundle() Bundle {
	return Bundle{
		Name:        "notes",
		Description: "Write and read notes. Use when the user asks to keep or find a note.",
		Version:     "v1.2.3",
		Tools: []Tool{
			Use(noteTool("note_read", "read", agenttool.WithAnnotations(agenttool.Annotations{ReadOnly: true}))),
			Use(noteTool("note_purge", "write"), Hidden()),
			Use(agenttool.New("note_count", "Count the notes.",
				func(ctx context.Context, _ agenttool.NoArgs) (string, error) { return "0", nil })),
		},
	}
}

// factsAnswer is what the program's facts command answers for a call
// of tool, built from agenttool.FactsOf: the answer the same tool gives
// in process.
func factsAnswer(t *testing.T, tool agenttool.Tool, args string) string {
	t.Helper()
	f, claims, err := agenttool.FactsOf(context.Background(), tool, json.RawMessage(args))
	if err != nil || !claims {
		t.Fatalf("FactsOf(%s): claims %v, err %v", args, claims, err)
	}
	type call struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
		Text string          `json:"text"`
	}
	calls := make([]call, len(f.Calls))
	for i, c := range f.Calls {
		calls[i] = call{c.Tool, c.Args, c.Text}
	}
	data, err := json.Marshal(map[string]any{"facts": map[string]any{"calls": calls}, "replay": "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFacts(t *testing.T) {
	b := notesBundle()
	read, purge := b.Tools[0].tool, b.Tools[1].tool
	tests := []struct {
		name   string
		args   []string
		status int
		stdout string // the JSON stdout holds, or "" for none
		stderr string // a substring of stderr
	}{
		{name: "as in process", args: []string{"facts", "note_read", "--name", "hr-review"}, stdout: factsAnswer(t, read, `{"name":"hr-review"}`)},
		{name: "from JSON", args: []string{"facts", "note_read", `{"name": "hr-review"}`}, stdout: factsAnswer(t, read, `{"name":"hr-review"}`)},
		{name: "after the program's options", args: []string{"--answer", "accept", "facts", "note_read", "--name", "hr-review"}, stdout: factsAnswer(t, read, `{"name":"hr-review"}`)},
		{name: "a hidden tool's", args: []string{"facts", "note_purge", "--name", "old"}, stdout: factsAnswer(t, purge, `{"name":"old"}`)},
		{name: "a hidden tool's after options", args: []string{"facts", "--record", "r", "note_purge", "--name", "old"}, stdout: factsAnswer(t, purge, `{"name":"old"}`)},
		{name: "no claim", args: []string{"facts", "note_count"}, stdout: `{"replay":"unknown"}`},
		{name: "a line not understood", args: []string{"facts", "note_read", "--title", "x"}, status: cli.ExitUsage, stderr: "title"},
		{name: "an unknown command", args: []string{"facts", "note_nope"}, status: cli.ExitUsage, stderr: `unknown command "note_nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, b, "", tt.args...)
			if r.status != tt.status {
				t.Fatalf("status %d, want %d\nstdout: %s\nstderr: %s", r.status, tt.status, r.stdout, r.stderr)
			}
			if got := strings.TrimSpace(r.stdout); got != tt.stdout {
				t.Errorf("stdout %s, want %s", got, tt.stdout)
			}
			if !strings.Contains(r.stderr, tt.stderr) {
				t.Errorf("stderr %q does not hold %q", r.stderr, tt.stderr)
			}
		})
	}
}

// help marks a command whose tool claims facts, and leaves the rest
// unmarked.
func TestFactsMarked(t *testing.T) {
	r := run(t, notesBundle(), "", "help")
	if r.status != cli.ExitOK {
		t.Fatalf("status %d: %s", r.status, r.stderr)
	}
	var read, count bool
	for _, line := range strings.Split(r.stdout, "\n") {
		switch {
		case strings.Contains(line, "`note_read`"):
			read = true
			if !strings.Contains(line, "Makes a facts claim.") {
				t.Errorf("note_read is not marked: %q", line)
			}
		case strings.Contains(line, "`note_count`"):
			count = true
			if strings.Contains(line, "facts") {
				t.Errorf("note_count is marked: %q", line)
			}
		case strings.Contains(line, "note_purge"):
			t.Errorf("help lists the hidden note_purge: %q", line)
		}
	}
	if !read || !count {
		t.Errorf("help lists note_read %v, note_count %v; want both:\n%s", read, count, r.stdout)
	}
}
