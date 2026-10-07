package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
