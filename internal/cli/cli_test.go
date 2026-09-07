package cli

import (
	"bytes"
	"strings"
	"testing"
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

// The tree-shaped conformance rules — grouping, Short style, placeholders, flag
// types, completion — live in cmd/olr/conformance_test.go, not here.
//
// They have to, and the reason is worth recording: NewRoot mounts the
// operations stubs, `daemon` and `version`, and no modules — those are added by
// cmd/olr. The versions of those tests that used to live in this file therefore
// walked a tree containing none of the commands an operator types, and passed
// for years while the three module surfaces drifted apart. Anything asserting
// about *the CLI* belongs where the whole CLI is visible.

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
