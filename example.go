package toolbundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ChristopherDavenport/agenttool"
	"github.com/ChristopherDavenport/agenttool/cli"
)

// checkExample parses an example's command line against c, the command
// it shows, with the parser that will run it: a stand-in tool with c's
// name and schema is run in its place, so the example passes exactly
// when the program would start the call. Required properties are not
// checked, as the parser does not check them.
func checkExample(ctx context.Context, program string, c cli.Command, args string) error {
	words, err := splitWords(args)
	if err != nil {
		return err
	}
	if len(words) == 0 || words[0] != c.Name {
		return fmt.Errorf("it must start with the command, %s", c.Name)
	}
	ran := false
	stand := agenttool.NewFunc(c.Name, c.Description, c.Schema, func(context.Context, agenttool.Call) (agenttool.Result, error) {
		ran = true
		return agenttool.Text(""), nil
	})
	var stderr strings.Builder
	r := cli.Runner{
		Name:   program,
		Tools:  agenttool.Set{stand},
		Stdin:  noStdin{},
		Stdout: io.Discard,
		Stderr: &stderr,
	}
	status := r.Run(ctx, words)
	switch {
	case status != cli.ExitOK:
		return errors.New(strings.TrimSpace(stderr.String()))
	case !ran:
		return errors.New("it is not a call of the command")
	}
	return nil
}

// noStdin is the standard input of an example, which has none to give.
type noStdin struct{}

func (noStdin) Read([]byte) (int, error) {
	return 0, errors.New("an example gives its JSON argument inline, not on stdin")
}

// splitWords splits a command line into words as a POSIX shell does,
// for the subset an example may use: words separated by blanks, single
// quotes taken literally, and double quotes in which a backslash
// escapes a backslash, a double quote, a dollar or a backquote. Outside
// quotes a word holds only characters no shell treats specially, so an
// example cannot teach a redirection, a pipe, an expansion or a glob.
func splitWords(s string) ([]string, error) {
	var words []string
	var w strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == ' ' || ch == '\t':
			if inWord {
				words = append(words, w.String())
				w.Reset()
				inWord = false
			}
		case ch == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("unterminated single quote")
			}
			w.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case ch == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				switch s[i] {
				case '\\':
					if i+1 < len(s) && strings.IndexByte("\\\"$`", s[i+1]) >= 0 {
						i++
					}
				case '$', '`':
					return nil, fmt.Errorf("%q in double quotes is expanded by the shell; use single quotes", s[i])
				}
				w.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New("unterminated double quote")
			}
			inWord = true
		case plain(ch) && (inWord || ch != '='):
			w.WriteByte(ch)
			inWord = true
		default:
			return nil, fmt.Errorf("%q must be quoted", ch)
		}
	}
	if inWord {
		words = append(words, w.String())
	}
	return words, nil
}

// plain reports whether a shell takes ch as itself outside quotes. An
// equals sign is plain except at the start of a word, where zsh expands
// it to a command's path.
func plain(ch byte) bool {
	switch {
	case 'a' <= ch && ch <= 'z', 'A' <= ch && ch <= 'Z', '0' <= ch && ch <= '9':
		return true
	}
	return strings.IndexByte("-_./:=,@+%", ch) >= 0
}
