package firewall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

var errNativeValidation = errors.New("invalid native nftables ruleset")

// ValidateNative checks ownership and declarative structure without touching
// the kernel. Full expression validation still runs before nft applies it.
func ValidateNative(raw []byte) error {
	_, err := nativeCommands(raw)
	return err
}

// nativeCommands accepts nft's declarative listing format, without interpreting
// expressions. Only our table may be replaced; arbitrary commands are refused.
func nativeCommands(raw []byte) ([]any, error) {
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid nftables JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one nftables JSON document")
	}
	var commands []any
	tables := 0
	for _, entry := range doc.Nftables {
		if len(entry) != 1 {
			return nil, fmt.Errorf("each nftables entry must contain one object")
		}
		for kind, value := range entry {
			if kind == "metainfo" {
				continue
			}
			switch kind {
			case "table", "chain", "rule", "set", "map", "element", "counter", "quota", "limit", "ct helper", "ct timeout", "ct expectation", "flowtable":
			default:
				return nil, fmt.Errorf("unsupported nftables object %q; use a declarative table listing, not commands", kind)
			}
			var obj map[string]any
			objectDecoder := json.NewDecoder(bytes.NewReader(value))
			objectDecoder.UseNumber()
			if err := objectDecoder.Decode(&obj); err != nil || obj == nil {
				return nil, fmt.Errorf("invalid %s object", kind)
			}
			if obj["family"] != "inet" {
				return nil, fmt.Errorf("only family inet may be edited")
			}
			if kind == "table" {
				if obj["name"] != TableName {
					return nil, fmt.Errorf("only table %s may be edited", TableName)
				}
				tables++
			} else if obj["table"] != TableName {
				return nil, fmt.Errorf("only table %s may be edited", TableName)
			}
			// Kernel-assigned identity and read-only reference counts are not input.
			delete(obj, "handle")
			delete(obj, "index")
			delete(obj, "use")
			if kind == "counter" {
				obj["packets"], obj["bytes"] = 0, 0
			}
			if kind == "rule" {
				if expressions, ok := obj["expr"].([]any); ok {
					for _, expression := range expressions {
						if statement, ok := expression.(map[string]any); ok {
							if counter, ok := statement["counter"].(map[string]any); ok {
								counter["packets"], counter["bytes"] = 0, 0
							}
						}
					}
				}
			}
			commands = append(commands, map[string]any{"add": map[string]any{kind: obj}})
		}
	}
	if tables != 1 {
		return nil, fmt.Errorf("include exactly one inet %s table", TableName)
	}
	return commands, nil
}

func runNFT(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nft %v: %w: %s", args, err, stderr.String())
	}
	return out, nil
}

type nativeView struct {
	Revision string          `json:"revision"`
	Ruleset  json.RawMessage `json:"ruleset"`
	Saved    json.RawMessage `json:"saved,omitempty"`
	Native   bool            `json:"native"`
}

func nativeRevision(current []byte, c Config) (string, error) {
	owned, found, err := ownedDocument(current)
	if err != nil {
		return "", err
	}
	var kernel []byte
	if found {
		kernel, err = canonicalNative(owned)
		if err != nil {
			return "", err
		}
	}
	saved, err := MarshalConfig(c)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(append(append(kernel, '\n'), saved...))), nil
}

func readNative(ctx context.Context) ([]byte, error) {
	return runNFT(ctx, nil, "-j", "list", "ruleset")
}

func ownedDocument(raw []byte) ([]byte, bool, error) {
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, false, err
	}
	entries := []any{}
	found := false
	for _, entry := range doc.Nftables {
		for kind, rawObject := range entry {
			var obj map[string]any
			if err := json.Unmarshal(rawObject, &obj); err != nil {
				return nil, false, err
			}
			if obj["family"] == "inet" && ((kind == "table" && obj["name"] == TableName) || obj["table"] == TableName) {
				entries = append(entries, entry)
				if kind == "table" {
					found = true
				}
			}
		}
	}
	out, err := json.Marshal(map[string]any{"nftables": entries})
	return out, found, err
}

func nativeBatch(raw, current []byte, enabled bool) ([]byte, error) {
	commands, err := nativeCommands(raw)
	if err != nil {
		return nil, err
	}
	_, found, err := ownedDocument(current)
	if err != nil {
		return nil, err
	}
	batch := []any{}
	if found {
		batch = append(batch, map[string]any{"delete": map[string]any{"table": map[string]any{"family": "inet", "name": TableName}}})
	}
	if enabled {
		batch = append(batch, commands...)
	}
	return json.Marshal(map[string]any{"nftables": batch})
}

func applyNative(ctx context.Context, d Desired) error {
	current, err := readNative(ctx)
	if err != nil {
		return err
	}
	batch, err := nativeBatch(d.Ruleset, current, d.Enabled)
	if err != nil {
		return err
	}
	_, err = runNFT(ctx, batch, "-j", "-f", "-")
	return err
}

// canonicalNative ignores only kernel identities and changing counter values.
// Expression order, chain priorities and rule order remain significant.
func canonicalNative(raw []byte) ([]byte, error) {
	commands, err := nativeCommands(raw)
	if err != nil {
		return nil, err
	}
	var clean func(any)
	clean = func(value any) {
		switch v := value.(type) {
		case []any:
			for _, child := range v {
				clean(child)
			}
		case map[string]any:
			if counter, ok := v["counter"]; ok && counter == nil {
				v["counter"] = map[string]any{}
			}
			if counter, ok := v["counter"].(map[string]any); ok {
				delete(counter, "packets")
				delete(counter, "bytes")
			}
			for _, child := range v {
				clean(child)
			}
		}
	}
	clean(commands)
	// Listings may group declarations differently. Order is only significant
	// among rules of the same chain; never sort those rules by their content.
	sort.SliceStable(commands, func(i, j int) bool {
		key := func(command any) string {
			for kind, value := range command.(map[string]any)["add"].(map[string]any) {
				obj := value.(map[string]any)
				if kind == "rule" {
					return "rule/" + fmt.Sprint(obj["chain"])
				}
				return kind + "/" + fmt.Sprint(obj["name"])
			}
			return ""
		}
		return key(commands[i]) < key(commands[j])
	})
	return json.Marshal(commands)
}

func (a Applier) planNative(ctx context.Context, c Config) (Plan, Desired, error) {
	current, err := readNative(ctx)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	// Validate the saved table even while disabled, so invalid expressions
	// cannot be stored unnoticed and fail on the next enable or startup.
	batch, err := nativeBatch(c.Ruleset, current, true)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	if _, err := runNFT(ctx, batch, "-j", "-c", "-f", "-"); err != nil {
		return Plan{}, Desired{}, fmt.Errorf("%w: %v", errNativeValidation, err)
	}
	owned, found, err := ownedDocument(current)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	empty := !c.Enabled && !found
	if c.Enabled && found {
		have, haveErr := canonicalNative(owned)
		want, wantErr := canonicalNative(c.Ruleset)
		empty = haveErr == nil && wantErr == nil && bytes.Equal(have, want)
	}
	p := Plan{Empty: empty, Impact: ImpactNone}
	if !empty {
		p.Impact = ImpactDisruptive
		p.Changes = []Change{{Kind: "replace", Line: "table inet " + TableName + " (native nftables JSON)"}}
		p.Warnings = []core.Problem{{Path: "ruleset", Message: "Replacing native rules can disconnect this browser. Review input rules and chain policies before applying; no automatic rollback is available."}}
	}
	return p, Desired{Enabled: c.Enabled, Ruleset: c.Ruleset}, nil
}
