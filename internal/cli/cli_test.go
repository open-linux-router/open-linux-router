package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestVerbPanicsOutsideVocabulary(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Verb accepted a word outside the shared vocabulary")
		}
	}()
	Verb("frobnicate", "")
}

func TestVerbUsesGenericShortWhenEmpty(t *testing.T) {
	if got := Verb("show", "").Short; got != verbs["show"] {
		t.Fatalf("Short = %q, want %q", got, verbs["show"])
	}
}

func TestHelpSucceeds(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	for _, want := range []string{"Modules:", "Operations:", "Local (work with olrd stopped):"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output missing group %q", want)
		}
	}
}

// Every top-level command must be grouped, or cobra files it under an untitled
// "Additional Commands" heading and the local/API split stops being visible.
func TestTopLevelCommandsAreGrouped(t *testing.T) {
	root := NewRoot()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	for _, c := range root.Commands() {
		if c.GroupID == "" {
			t.Errorf("command %q has no GroupID", c.Name())
		}
	}
}

func TestEveryCommandHasShortHelp(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Short == "" {
			t.Errorf("command %q has no Short description", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRoot())
}

func TestStubsReportNotImplemented(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status"})

	err := root.Execute()
	if err == nil {
		t.Fatal("stub command returned nil error; stubs must not look like success")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("error = %q, want it to mention 'not implemented'", err)
	}
	if !strings.Contains(err.Error(), "olr status") {
		t.Fatalf("error = %q, want it to name the command path", err)
	}
}
