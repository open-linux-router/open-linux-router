# `olr` CLI conventions

Status: **the rules below are enforced by `cmd/olr/conformance_test.go`.**
A command that breaks one fails the build rather than shipping. Section
references are to `design.md` unless prefixed `dns:` or `gateway:`.

This document exists because of one observation. `design.md` §6.1 put the shared
verb vocabulary in code — `cli.Verb` panics at startup on a word outside it —
and across three modules and forty-odd commands **the verbs never drifted**.
Everything else about the surface was left to reviewer attention: argument
placeholders, flag naming, how a boolean is spelled, what an empty list says.
All of it drifted, in every module, in a different direction.

That is not a story about discipline. It is a story about which rules were
executable. So this document's job is not to list preferences — it is to state
the rules in a form a test can check, and every section below ends with the
assertion that holds it.

---

## 1. The shape

```
olr <module> <verb> [<object>] [<name>] [flags]

  olr dhcp show pools
  olr dhcp add reservation <mac> --ip 192.168.1.50
  olr gateway set via <network> <exit>
  olr dns rm block <name>...
```

Four positions, and each answers a different question:

| Position | Question | Vocabulary |
|---|---|---|
| module | which subsystem | bounded, mounted in `cmd/olr` |
| verb | what kind of change | **closed set** (§3.2 rule 4) |
| object | what kind of thing | per module, singular for one, plural for a list |
| name | which one | positional, always |

The verb set is closed and enforced elsewhere (`internal/cli/verbs.go`). The
rules below govern the two positions to its right and the flags after them.

---

## 2. R1 — Placeholders are `<lower-case>`

> **Every argument placeholder is lower-case inside angle brackets. Optional
> arguments take square brackets outside the angle brackets. Variadic takes a
> trailing ellipsis.**

```
pool <interface>          required
default [<exit>]          optional
block <name>...           one or more
via <network> <exit>      two required
traffic-counting on|off   a closed literal choice
```

The last form is the exception, and it is deliberately narrow: a lower-case
alternation of literal words, for the `set <feature> on|off` shape R3 reserves.
`<state>` would be the consistent spelling and a worse one — the two words *are*
the vocabulary, and hiding them behind a placeholder means `--help` no longer
says what may be typed.

Three styles were in use — `<interface>` in dhcp, bare `NAME` in dns and
gateway, `[name]` in one dns command — and the bare form is the one worth
losing. In `--help`, `via NETWORK EXIT` renders as three words of the same
weight, and nothing tells the reader that the first is a subcommand and the
other two are things they must supply. Angle brackets are the man-page
convention for exactly this reason, and they also survive being pasted into
prose, which `NAME` does not.

Not `UPPER_CASE`, because the placeholders are frequently interface names and
MAC addresses, where case is either meaningful or actively misleading.

**Enforced by** `TestPlaceholderStyle`, and by
`TestOptionalPlaceholdersComeLast` — an optional placeholder before a required
one is not a shape a positional parser could honour anyway.

## 3. R2 — Identity is positional, never a flag

> **The name that says *which object* is always the first positional argument.
> Flags carry properties, never identity.**

```
olr dhcp add reservation <mac> --ip … --hostname …     yes
olr dhcp add reservation --mac … --ip …                no
```

This was the surface's sharpest inconsistency: `add reservation` took its
identity from `--mac` while `rm reservation` took the same identity
positionally, so the two halves of one object's lifecycle disagreed about what
a reservation *is*. Every other object in every other module — pools, policies,
exits — was already positional.

The argument is not symmetry. It is that a positional identity is the only kind
this CLI can complete (R7), the only kind that reads correctly in the
`no <object> named <name>` error template (R8), and the only kind that makes
`add`/`rm` a pair rather than two unrelated commands that happen to share a
noun.

Properties may still be required flags — `add reservation <mac> --ip …` needs
an address, and marking it required is right. The rule is about *identity*: the
answer to "which one" is a positional, and everything else is a flag.

**Enforced by** `TestArgsMatchPlaceholders`, which exercises each command's
`Args` validator against the arity its `Use` string advertises. That check earns
its place beyond style: a command promising `<network> <exit>` while validating
`ExactArgs(1)` is a real defect wearing a formatting defect's clothes.

## 4. R3 — Booleans have exactly three spellings

> **Module lifecycle → `enable` / `disable`. An independent subsystem →
> `set <feature> on|off`. A property of an object → paired `--x` / `--no-x`,
> and the positive flag always defaults to false.**

Four spellings were in use, and one of them was a trap. pflag does not accept a
space-separated value for a boolean, so `--snat false` parses as `--snat=true`
plus a positional argument `false`. On `add exit <name>`, which takes exactly
one positional, the resulting error complains about argument count — which is
both wrong and unrelated to what the operator got wrong. A flag whose only
correct spelling is `--snat=false` is a flag that will be typed incorrectly.

Hence: no boolean flag may default to true. Where the underlying field must
default to true, the flag is the negative one (`--no-snat`) and the config
struct keeps the true default. Where both directions must be expressible
against a tri-state field, both flags exist and are mutually exclusive.

`set <feature> on|off` is reserved for state with a lifetime of its own —
traffic counting survives routing being disabled, and vice versa, so it is
neither a module lifecycle nor a property of any object.

**Enforced by** `TestBoolFlagsDefaultFalse` and `TestNegativeFlagsArePaired`.

## 5. R4 — Clearing is `--no-<field>`

> **One way to unset an optional field: the paired negative flag from R3.**

Four ways were in use: `--no-gateway`, `--snat=false`, `--none`, and passing an
empty string to `--probe`. The last is the worst of them — `--probe ""` is
indistinguishable from a shell variable that expanded to nothing, so the
mechanism for clearing a field is also the mechanism by which a typo silently
clears it.

```
olr gateway set default --no-exit          was --none
olr gateway add exit <name> --no-probe     was --probe ""
olr dhcp set pool <if> --no-gateway        unchanged
```

**Enforced by** `TestNegativeFlagsArePaired`, same assertion as R3.

## 6. R5 — Multi-value flags are repeatable, never comma-split

> **Every multi-value flag is `StringArrayVar`, is documented `(repeatable)`,
> and does not split on commas.**

This is the one rule here that fixes a behavioural bug rather than a style
drift. `StringSliceVar` splits its input on commas; `StringArrayVar` does not.
The surface used Slice for eleven flags and Array for one — `--option`, which
had to be Array because a DHCP option value may legitimately contain a comma.

So the tool had two flags that looked identical in `--help` and behaved
differently, and the difference only showed up as *silently corrupted values*
in the case the operator was least likely to test. Standardising on Array costs
`--dns 1.1.1.1,8.8.8.8` and buys one mental model: a flag takes one value, and
you repeat it.

```
--dns 1.1.1.1 --dns 8.8.8.8
```

**Enforced by** `TestMultiValueFlagsAreArrays`: no `stringSlice`-typed flag may
be registered anywhere in the tree.

## 7. R6 — Every listable object has a list and a detail command

> **For each object a module stores: `show <objects>` lists them,
> `show <object> <name>` shows one in full.**

Three shapes were in use. dhcp had lists but no way to see a single pool; dns
had `show policies [name]`, one command doing both jobs; gateway had neither,
only a whole-module `show`.

`policies [name]` is the interesting failure. It cannot be completed sensibly —
the completion for the argument depends on whether the operator intends a list
or a detail, which is not knowable — and its `Short` has to describe two
behaviours in one line ("List policies, or show one in full"). Splitting it
costs one command and makes both halves describable.

`olr <module> show` with no object remains the whole-module view, and stays the
thing an operator reaches for first.

**Enforced by** `TestShowCommandsAreListOrDetail`: no `show` subcommand may
take an optional argument, and none may take more than one. Detecting English
plurals is not something to build a build failure on, so the assertion bans the
dual-purpose *shape* rather than trying to police the nouns.

## 8. R7 — Anything that names an object completes it

> **Every positional that takes a stored object's name registers a
> `ValidArgsFunction`; every enum-valued flag registers a completion.**

The tree had one `ValidArgs` and no `ValidArgsFunction` at all, while
registering cobra's `completion` command — so `olr completion bash` produced a
completer that knew every subcommand and no exit, interface, policy or MAC. On
a CLI whose primary objects are operator-chosen strings, that is most of the
value of completion missing.

Completion is a read of stored config, so it goes through the same client as
everything else (§6.1) and degrades to no suggestions when olrd is down, which
is the correct failure: `olr --help` still works with the daemon stopped (§6.1),
but *which exits exist* is a question only the daemon can answer.

`add` is exempt: it is naming a new object, so completing from what exists
would suggest exactly the names about to be rejected as duplicates. `<file>` is
exempt too — the shell's own path completion is the right answer there.

**Enforced by** `TestObjectPlaceholdersHaveCompletion`, over the `rm` and `set`
subtrees.

## 9. R8 — Four wording templates, one implementation

> **Empty state, no-op, unknown object, and partial failure each have exactly
> one phrasing, and it lives in `internal/cli`.**

| Situation | Template |
|---|---|
| empty list, stored | `No <objects> configured.` + a next step when one exists |
| empty list, observed | `No <objects> observed yet.` |
| nothing to apply | `Nothing to do; the configuration is already applied.` |
| unknown object | `no <object> named "<name>"` + `(have a, b, c)` when ≤ 8 exist |
| partial failure | `What was done before the failure:` then the steps |

The unknown-object template is the one worth centralising. Four phrasings were
in use across three modules, mixing `%s` and `%q`, and only gateway's listed the
candidates that do exist — which is the half of the answer that matters, because
the reason an operator lands here is almost always a typo rather than a genuine
absence. `cli.UnknownObject` is that function, and it is now the only way to
phrase the error.

Object names are always `%q`. An unquoted interface name with a trailing space
is invisible in an error message, and trailing whitespace in a copy-pasted name
is a real way to arrive here.

Empty lists split by kind because "configured" would be a lie about leases and
queries, and the difference is the one an operator most needs: an empty
reservation list means nobody wrote one, an empty lease list means nobody asked
for one. Same shape on screen, opposite thing to do about it. `cli.NoObjects`
and `cli.NoneObserved` are the two.

**Not enforced by a test.** These are four functions in `internal/cli` and the
rule is that modules call them, which no assertion checks — a module could still
format its own. The `--help` goldens catch drift in command text but say nothing
about runtime output. Worth revisiting if a fourth module makes it a pattern
rather than an oversight.

## 10. R9 — The `-o` and `--dry-run` contracts

> **Any command that can emit `-o json` validates the flag before doing work.
> `--dry-run` has three meanings and no fourth, and a command it cannot mean
> anything for rejects it.**

`--output` was validated in nine places and skipped in four, all four in dhcp —
so `olr dhcp show -o yaml` printed text and `olr dns show -o yaml` errored. The
same flag on the same tool.

`--dry-run` is a persistent root flag whose help promises "show what would
change and exit without applying it". Its three legitimate meanings:

| On | Means |
|---|---|
| a mutating command | plan the edit, print it, apply nothing |
| `show` | plan stored intent against reality — the drift question (§5.4) |
| anything else | **error** |

The third row is the change. `--dry-run` was silently ignored by `olr start`,
`olr version`, and every dhcp read command. Silently ignoring it is
strictly worse than rejecting it, because the operator who types it before a
disruptive change is the operator who most needs to be told it did not apply.

**Enforced by** `TestEveryJSONCommandValidatesOutput` and
`TestDryRunIsHandledOrRejected`.

---

## 11. Enforcement

The rules above are checked by a single tree walk. It matters *which* tree.

`internal/cli/cli_test.go` walked `NewRoot()`, and `NewRoot` mounts the
operations stubs, `daemon` and `version` — the modules are mounted by
`cmd/olr/main.go`. So every conformance test in the repo was walking a tree that
contained none of the commands an operator actually uses, and had been passing
for that reason. `TestEveryCommandHasShortHelp` had never once looked at dhcp.

The fix is `newRoot()` in `cmd/olr`, which `main` calls too, so the tree under
test and the tree an operator gets cannot diverge. The conformance tests live in
that package for the same reason — `internal/cli` cannot import the modules
(they import it), so it is structurally incapable of seeing the tree it defines
the rules for.

The walk skips cobra's generated `help` and `completion` subtrees: their text
and argument shapes are not ours, and asserting on them would turn a cobra
upgrade into a conformance failure.

Two kinds of check:

1. **Structural**, per rule above — placeholder syntax, flag types and
   defaults, completion presence, `Args` matching the placeholder count.
2. **Golden `--help` output**, one file per module under
   `internal/cli/testdata/`. Help text is the contract this document is about,
   so a change to it should show up as a diff in review rather than as a
   surprise for whoever reads it next.

`Args` is a function rather than data, so the arity check *exercises* it —
calling each validator with one argument too few and one too many — rather than
inspecting a declaration. What it asserts is what the command would actually
accept.

---

## 12. What this breaks

`VERSION` is `0.1.0`. Every breaking change here is cheap now and expensive
after 1.0, which is the argument for doing all of them in one batch rather than
discovering the need for a deprecation cycle later.

| Was | Is | Rule |
|---|---|---|
| `add reservation --mac <mac>` | `add reservation <mac>` | R2 |
| `add exit <n> --snat=false` | `add exit <n> --no-snat` | R3 |
| `set default --none` | `set default --no-exit` | R4 |
| `add exit <n> --probe ""` | `add exit <n> --no-probe` | R4 |
| `--dns 1.1.1.1,8.8.8.8` | `--dns 1.1.1.1 --dns 8.8.8.8` | R5 |
| `show policies <name>` | `show policy <name>` | R6 |

Stored configuration is untouched by all of it: these are argument-parsing
changes, and `olr.json` never sees them.

---

## 13. Open

1. **`set` has two shapes and this document does not pick one.** dhcp and
   gateway use `set` as a pure group (`set pool <if> --…`); dns uses it as a
   leaf with eleven flags (`set --mode … --upstream …`). Both are internally
   consistent, and the split tracks a real difference — dns's settings are a
   module-wide singleton with no object to name. But eleven flags on one command
   is where a fifth would not fit, and dns's are really four subsystems
   (listen / upstream / query-log / redirect). Splitting them is a bigger change
   than anything above and wants its own argument.

2. **Errors are not machine-readable, so `-o json` is only half a contract.**
   `cmd/olr` prints `olr: <prose>` to stderr regardless of `--output`, and the
   per-field paths in `APIError.Problems` — which exist precisely so a caller
   can tell *which field* was rejected — never reach a machine. Fixing it is
   small and is not in this document because it is an exit-code and
   error-envelope decision, not a naming convention.

3. **One exit code.** `cmd/olr` exits 1 for everything, so a script cannot
   separate "olrd is down" from "your input was rejected". `APIError` already
   carries the status. Same batch as (2) when that happens.
