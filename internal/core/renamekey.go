package core

import (
	"encoding/json"
	"fmt"
)

// RenameKey renames one key of a JSON object, for a module reading a subtree
// written before one of its fields was renamed.
//
// Why a rename needs this at all: every module decodes its subtree with
// DisallowUnknownFields, so a key we used to write is indistinguishable from a
// typo, and an upgraded box would refuse to load the file it wrote itself. The
// module renames the old key before its strict decode, and internal/daemon
// rewrites the document once at startup so the old spelling does not outlive
// the upgrade.
//
// It reports whether it renamed anything. Data that is not an object, or that
// does not carry the old key, comes back unchanged and unreported: telling a
// malformed subtree from a current one is the strict decode's job, and that
// decode is the one with the context to say what is wrong.
//
// Both keys at once is an error rather than a choice. A subtree that says two
// things under two spellings was edited by hand after the rename, and picking
// either one would silently discard what somebody typed.
func RenameKey(data []byte, from, to string) ([]byte, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return data, false, nil
	}
	old, ok := obj[from]
	if !ok {
		return data, false, nil
	}
	if _, both := obj[to]; both {
		return data, false, fmt.Errorf("both %q and %q are set; %q is the old name for %q, so remove it",
			from, to, from, to)
	}
	delete(obj, from)
	obj[to] = old
	out, err := json.Marshal(obj)
	if err != nil {
		return data, false, err
	}
	return out, true, nil
}
