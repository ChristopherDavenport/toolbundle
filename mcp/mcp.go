// Package mcp serves a bundle's tools over MCP on stdin and stdout, as
// a command of the bundle's program. One binary is then both the
// program a model calls through a shell, with the skill export-skill
// writes, and the server an MCP client starts:
//
//	os.Exit(toolbundle.Main(context.Background(), toolbundle.Bundle{
//		Name:        "file-tools",
//		Description: "Read, search and edit files in the working tree. Use when …",
//		Tools:       []toolbundle.Tool{ … },
//		Commands:    []toolbundle.Command{mcp.Command()},
//	}, os.Args))
//
//	claude mcp add file-tools -- file-tools mcp
//
// It is a module of its own so that the MCP SDK reaches only the
// programs that serve MCP. A bundle without this command carries none
// of it, which matters where a host forbids MCP and reviews what it
// installs.
//
// The server is [mcpserver]'s, so a tool reaches an MCP client as it
// would from any server built on it: questions are elicitations,
// progress is notifications and records are _meta. It serves the tools
// the skill shows; a hidden tool is not served, since MCP has no tool
// a client may call but not list. One process serves the whole
// session, so a tool that keeps state between calls keeps it here, as
// it does not as one call per process. The program closes the tools
// when the session ends.
package mcp

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/ChristopherDavenport/agenttool/cli"
	"github.com/ChristopherDavenport/agenttool/mcpserver"
	"github.com/ChristopherDavenport/toolbundle"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Name is the word that runs the command: <program> mcp.
const Name = "mcp"

// Command is the mcp command, with mcpserver's default options.
func Command() toolbundle.Command {
	return CommandWith(mcpserver.Options{})
}

// CommandWith is the mcp command, serving the tools with o.
//
// It takes no arguments, and refuses any with [cli.ExitUsage], as it
// does a tool mcpserver will not serve. It serves one session on the
// program's standard streams, named and versioned as the bundle, until
// the client closes stdin or the program is interrupted or terminated,
// and exits with [cli.ExitOK]. A session that ends with an error
// reports it on stderr and exits with [cli.ExitFailed]. Nothing but the
// protocol is written to stdout.
func CommandWith(o mcpserver.Options) toolbundle.Command {
	return toolbundle.Command{Name: Name, Run: func(ctx context.Context, p toolbundle.Program, args []string) int {
		if len(args) > 0 {
			fmt.Fprintf(p.Stderr, "%s: %s takes no arguments\n", p.Name, Name)
			return cli.ExitUsage
		}
		srv, err := o.NewServer(p.Name, p.Version, p.Tools...)
		if err != nil {
			fmt.Fprintf(p.Stderr, "%s: %v\n", p.Name, err)
			return cli.ExitUsage
		}
		// A client stops a stdio server by closing its stdin, and then
		// by terminating it. Either way the program goes on to close the
		// tools.
		ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM)
		defer stop()
		if err := srv.Run(ctx, transport(p)); err != nil && ctx.Err() == nil {
			fmt.Fprintf(p.Stderr, "%s: %v\n", p.Name, err)
			return cli.ExitFailed
		}
		return cli.ExitOK
	}}
}

// transport is [sdk.StdioTransport] over the program's streams: stdin
// is closed when the session ends, which is what stops the read of a
// session the server ended itself, and stdout is left open.
func transport(p toolbundle.Program) sdk.Transport {
	in, ok := p.Stdin.(io.ReadCloser)
	if !ok {
		in = io.NopCloser(p.Stdin)
	}
	return &sdk.IOTransport{Reader: in, Writer: nopCloser{p.Stdout}}
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
