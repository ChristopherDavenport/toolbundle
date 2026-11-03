package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ChristopherDavenport/agenttool"
	"github.com/ChristopherDavenport/agenttool/cli"
	"github.com/ChristopherDavenport/agenttool/mcpclient"
	"github.com/ChristopherDavenport/agenttool/mcpserver"
	"github.com/ChristopherDavenport/toolbundle"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// childEnv, when set, makes the test binary the counter program, so
// the tests run the command as a client does: a process on the other
// end of stdin and stdout, started through toolbundle.Main.
const childEnv = "TOOLBUNDLE_MCP_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) != "" {
		os.Exit(toolbundle.Main(context.Background(), counter(), append([]string{"counter"}, os.Args[1:]...)))
	}
	os.Exit(m.Run())
}

// counter is a bundle with a tool that keeps state, one that asks a
// question and one that is hidden.
func counter() toolbundle.Bundle {
	n := 0
	count := agenttool.New("count", "Count the calls of this tool.",
		func(ctx context.Context, _ agenttool.NoArgs) (string, error) {
			n++
			return strconv.Itoa(n), nil
		}, agenttool.WithCloser(func() error {
			fmt.Fprintln(os.Stderr, "closed count")
			return nil
		}))
	confirm := agenttool.New("confirm", "Ask before acting.",
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
	reset := agenttool.New("reset", "Reset the count.",
		func(ctx context.Context, _ agenttool.NoArgs) (string, error) { n = 0; return "reset", nil })
	return toolbundle.Bundle{
		Name:        "counter",
		Description: "Count calls. Use when a task needs counting.",
		Version:     "v1.2.3",
		Tools: []toolbundle.Tool{
			toolbundle.Use(count, toolbundle.Stateful()),
			toolbundle.Use(confirm),
			toolbundle.Use(reset, toolbundle.Hidden()),
		},
		Commands: []toolbundle.Command{Command()},
	}
}

// program is the counter program run with args.
func program(t *testing.T, args ...string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), childEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd, &stderr
}

// connect starts the counter program's mcp command and opens a session
// with it, as a client whose user accepts every question.
func connect(t *testing.T) (*sdk.ClientSession, *exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd, stderr := program(t, Name)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, &sdk.ClientOptions{
		ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			return &sdk.ElicitResult{Action: "accept"}, nil
		},
	})
	cs, err := client.Connect(context.Background(), &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v\nstderr: %s", err, stderr)
	}
	return cs, cmd, stderr
}

func call(t *testing.T, cs *sdk.ClientSession, name string) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func TestServe(t *testing.T) {
	cs, _, stderr := connect(t)

	if info := cs.InitializeResult().ServerInfo; info.Name != "counter" || info.Version != "v1.2.3" {
		t.Errorf("server is %s %s, want counter v1.2.3", info.Name, info.Version)
	}
	if got, want := cs.InitializeResult().Instructions, counter().Description; got != want {
		t.Errorf("instructions %q, want the bundle's description %q", got, want)
	}

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"confirm", "count"}) {
		t.Errorf("tools %v, want confirm and count, the hidden reset left out", names)
	}

	for _, want := range []string{"1", "2", "3"} {
		if got, isErr := call(t, cs, "count"); got != want || isErr {
			t.Errorf("count = %q (error %v), want %q: one process keeps the state", got, isErr, want)
		}
	}
	if got, isErr := call(t, cs, "confirm"); got != "answered accept" || isErr {
		t.Errorf("confirm = %q (error %v), want the client's answer", got, isErr)
	}
	if _, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "reset"}); err == nil {
		t.Error("the hidden reset ran over MCP")
	}

	// Closing stdin ends the session; the program then closes the tools
	// and exits 0, which Close reports as no error.
	if err := cs.Close(); err != nil {
		t.Errorf("close: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr.String(), "closed count") {
		t.Errorf("the tools were not closed; stderr: %q", stderr)
	}
}

func TestServeTerminated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM to send")
	}
	cs, cmd, stderr := connect(t)
	if got, _ := call(t, cs, "count"); got != "1" {
		t.Fatalf("count = %q", got)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cs.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("terminated: %v\nstderr: %s", err, stderr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the program did not exit on SIGTERM")
	}
	if !strings.Contains(stderr.String(), "closed count") {
		t.Errorf("the tools were not closed; stderr: %q", stderr)
	}
}

func TestRefuses(t *testing.T) {
	bad := agenttool.NewFunc("bad", "Bad.",
		[]byte(`{"type":"object","properties":{"x":{"$ref":"#/nowhere"}}}`),
		func(context.Context, agenttool.Call) (agenttool.Result, error) { return agenttool.Text(""), nil })
	tests := []struct {
		name  string
		tools agenttool.Set
		args  []string
		want  string
	}{
		{"arguments", nil, []string{"--http", ":8080"}, "counter: mcp takes no arguments"},
		{"a schema mcpserver will not serve", agenttool.Set{bad}, nil, `counter: mcpserver: tool "bad"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			p := toolbundle.Program{Name: "counter", Tools: tt.tools, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}
			if status := Command().Run(context.Background(), p, tt.args); status != cli.ExitUsage {
				t.Errorf("status %d, want %d; stderr %q", status, cli.ExitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr %q does not hold %q", stderr.String(), tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout %q", stdout.String())
			}
		})
	}
}

func TestRefusesThroughMain(t *testing.T) {
	cmd, stderr := program(t, Name, "extra")
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != cli.ExitUsage {
		t.Fatalf("exit %v, want status 2; stderr %q", err, stderr)
	}
	if !strings.Contains(stderr.String(), "counter: mcp takes no arguments") {
		t.Errorf("stderr %q", stderr)
	}
}

// inMemory runs cmd for p on pipes in place of the program's standard
// streams, and returns the client's end of them. The command runs until
// the client closes its end; the test then checks that it exited with
// cli.ExitOK.
func inMemory(t *testing.T, cmd toolbundle.Command, p toolbundle.Program) sdk.Transport {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p.Stdin, p.Stdout, p.Stderr = inR, outW, io.Discard
	done := make(chan int, 1)
	go func() {
		status := cmd.Run(context.Background(), p, nil)
		outW.Close()
		done <- status
	}()
	t.Cleanup(func() {
		inW.Close()
		select {
		case status := <-done:
			if status != cli.ExitOK {
				t.Errorf("the command exited %d", status)
			}
		case <-time.After(10 * time.Second):
			t.Error("the command did not end when the client closed")
		}
	})
	return &sdk.IOTransport{Reader: outR, Writer: inW}
}

// noteRead is a tool that claims facts: the note file a call reads.
func noteRead() agenttool.Tool {
	type nameArgs struct {
		Name string `json:"name" desc:"The note's name"`
	}
	return agenttool.New("note_read", "Read a note.",
		func(ctx context.Context, a nameArgs) (string, error) { return "read " + a.Name, nil },
		agenttool.WithAnnotations(agenttool.Annotations{ReadOnly: true}),
		agenttool.WithFacts(func(_ context.Context, args json.RawMessage) (agenttool.Facts, error) {
			a, err := agenttool.Decode[nameArgs](args)
			if err != nil {
				return agenttool.Facts{}, err
			}
			path, _ := json.Marshal(map[string]string{"path": ".notes/" + a.Name + ".json"})
			return agenttool.Facts{Calls: []agenttool.FactCall{{Tool: "read", Args: path, Text: "read .notes/" + a.Name + ".json"}}}, nil
		}))
}

// A client that takes the server's claims finds a served tool's facts
// claim, and is answered what the tool answers in process. mcpserver
// serves the claim from v0.0.20; this fails if what the module selects
// falls below it.
func TestServeFacts(t *testing.T) {
	ctx := context.Background()
	local := noteRead()
	p := toolbundle.Program{Name: "notes", Version: "v1.2.3", Tools: agenttool.Set{local}}
	remote, err := mcpclient.Connect(ctx, inMemory(t, Command(), p), mcpclient.WithClaims())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { remote.Close() })

	tools := remote.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools %v, want note_read", tools)
	}
	if !agenttool.IsFactual(tools[0]) {
		t.Fatal("the served note_read makes no facts claim")
	}
	args := json.RawMessage(`{"name":"hr-review"}`)
	got, claims, err := agenttool.FactsOf(ctx, tools[0], args)
	if err != nil || !claims {
		t.Fatalf("facts: claims %v, err %v", claims, err)
	}
	want, _, _ := agenttool.FactsOf(ctx, local, args)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Errorf("facts over MCP %s, want %s as in process", gotJSON, wantJSON)
	}
}

// The server's instructions are the bundle's description, unless the
// caller gives its own.
func TestInstructions(t *testing.T) {
	const description = "Count calls. Use when a task needs counting."
	tests := []struct {
		name string
		cmd  toolbundle.Command
		want string
	}{
		{"the bundle's description", Command(), description},
		{"the caller's own", CommandWith(mcpserver.Options{Instructions: "Call count to count."}), "Call count to count."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := toolbundle.Program{Name: "counter", Version: "v1.2.3", Description: description}
			remote, err := mcpclient.Connect(context.Background(), inMemory(t, tt.cmd, p))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { remote.Close() })
			if got := remote.Instructions(); got != tt.want {
				t.Errorf("instructions %q, want %q", got, tt.want)
			}
		})
	}
}
