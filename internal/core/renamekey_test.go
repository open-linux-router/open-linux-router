package core

import (
	"encoding/json"
	"testing"
)

func TestRenameKeyMovesTheValue(t *testing.T) {
	out, renamed, err := RenameKey([]byte(`{"groups":[1,2],"adopted":["a"]}`), "groups", "networks")
	if err != nil || !renamed {
		t.Fatalf("renamed = %v, err = %v", renamed, err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["groups"]; ok {
		t.Errorf("the old key survived: %s", out)
	}
	if string(got["networks"]) != `[1,2]` || string(got["adopted"]) != `["a"]` {
		t.Errorf("values were not carried across: %s", out)
	}
}

// A current subtree, and one that is not an object at all, both come back as
// they were: the strict decode that follows is what reports a malformed one.
func TestRenameKeyLeavesOtherDataAlone(t *testing.T) {
	for _, in := range []string{`{"networks":[]}`, `[1]`, `not json`, `null`} {
		out, renamed, err := RenameKey([]byte(in), "groups", "networks")
		if err != nil || renamed || string(out) != in {
			t.Errorf("%s: out = %s, renamed = %v, err = %v", in, out, renamed, err)
		}
	}
}

func TestRenameKeyRefusesBothSpellings(t *testing.T) {
	if _, _, err := RenameKey([]byte(`{"groups":[],"networks":[]}`), "groups", "networks"); err == nil {
		t.Fatal("accepted a subtree carrying both the old and the new key")
	}
}
