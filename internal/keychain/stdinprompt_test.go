package keychain

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadPasswordStdinEcho(t *testing.T) {
	// When stdin is not a terminal (typical test environment), the function
	// reads a line of plain text. We exercise that path by passing an
	// io.Reader.
	in := strings.NewReader("hunter2\n")
	var promptOut bytes.Buffer

	pw, err := readPasswordFrom(in, &promptOut, "Enter password: ")
	if err != nil {
		t.Fatalf("readPasswordFrom: %v", err)
	}
	if pw != "hunter2" {
		t.Errorf("password = %q, want %q", pw, "hunter2")
	}
	if !strings.Contains(promptOut.String(), "Enter password:") {
		t.Errorf("prompt not written; got %q", promptOut.String())
	}
}

func TestReadPasswordStdinTrimsCRLF(t *testing.T) {
	in := strings.NewReader("secret\r\n")
	var promptOut bytes.Buffer

	pw, err := readPasswordFrom(in, &promptOut, "Pw: ")
	if err != nil {
		t.Fatalf("readPasswordFrom: %v", err)
	}
	if pw != "secret" {
		t.Errorf("password = %q, want %q", pw, "secret")
	}
}

func TestReadPasswordStdinEmpty(t *testing.T) {
	in := strings.NewReader("\n")
	var promptOut bytes.Buffer

	pw, err := readPasswordFrom(in, &promptOut, "Pw: ")
	if err != nil {
		t.Fatalf("readPasswordFrom: %v", err)
	}
	if pw != "" {
		t.Errorf("password = %q, want empty", pw)
	}
}
