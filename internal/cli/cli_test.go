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
	for _, want := range []string{"Modules:", "Operations:", "Service:", "Other:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output missing group %q", want)
		}
	}

	// The word the flattening was for. `daemon` is still the vocabulary in the
	// code and in design.md §4.2, where the module/backend distinction needs
	// it — but an operator has one program installed and should not have to
	// learn it in order to restart that program.
	if strings.Contains(out.String(), "daemon") {
		t.Error("help output says \"daemon\"; the service commands are top-level now")
	}
}

// The tree-shaped conformance rules — grouping, Short style, placeholders, flag
// types, completion — live in cmd/olr/conformance_test.go, not here.
//
// They have to, and the reason is worth recording: NewRoot mounts the
// operations stubs, the service commands and `version`, and no modules — those
// are added by cmd/olr. The versions of those tests that used to live in this
// file therefore walked a tree containing none of the commands an operator
// types, and passed for years while the three module surfaces drifted apart.
// Anything asserting about *the CLI* belongs where the whole CLI is visible.

// `diff`, not `status`. `status` used to be a stub and is a real command now:
// flattening the daemon group merged it with `olr daemon status`, which had
// been quietly answering the liveness half of what the stub advertised.
func TestStubsReportNotImplemented(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"diff"})

	err := root.Execute()
	if err == nil {
		t.Fatal("stub command returned nil error; stubs must not look like success")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("error = %q, want it to mention 'not implemented'", err)
	}
	if !strings.Contains(err.Error(), "olr diff") {
		t.Fatalf("error = %q, want it to name the command path", err)
	}
}
