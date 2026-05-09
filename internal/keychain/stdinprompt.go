package keychain

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPasswordStdin prompts the user on os.Stderr and reads a password
// from os.Stdin. When stdin is a terminal, input is read with echo
// disabled. When stdin is a pipe (e.g., scripted use), a newline-terminated
// line is read with normal buffering.
func ReadPasswordStdin(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		// Always emit a newline after a no-echo read so the next line of
		// terminal output isn't on the same row as the (invisible) input.
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return readPasswordFrom(os.Stdin, os.Stderr, prompt)
}

// readPasswordFrom is the non-terminal code path, factored out so tests
// can supply their own reader/writer.
func readPasswordFrom(in io.Reader, promptOut io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(promptOut, prompt); err != nil {
		return "", err
	}
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
