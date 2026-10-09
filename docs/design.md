# Matching and execution

The executable stays outside swm's daemon. It reads `query windows`, `query spaces`,
and `query displays`, uses the TypeSafe HTTP API, and invokes swm with argument
arrays. It does not access private macOS APIs directly.

## Candidates

Candidates must have a known PID and app name, an Accessibility reference, a
standard window role, and a normal window layer. Minimized, fullscreen, dialog,
and multi-Space windows are excluded. Membership comes from every Space's window
list; the single `space` field on a window cannot identify sticky windows alone.

App exclusions apply before any metadata reaches TypeSafe. Local window IDs,
PIDs, display IDs, and Space IDs stay local. The request includes opaque keys,
app names, titles, and whether each window belongs to the current or another
desktop Space. More than 254 candidates or an oversized request produces an
error instead of silently discarding candidates.

Titles need useful metadata. A terminal title containing a project name supports
project selection. The helper cannot inspect processes inside terminals, hidden
browser tabs, document contents, or screenshots.

## Decision policy

One TypeSafe request asks a Choice question about the best supported window and
a Noul question about whether any match exists. Choice includes a `none` option.
This follows TypeSafe's [semantic search pattern](https://docs.typesafe.ai/cookbooks/semantic_find).

The client checks answer types, required fields, candidate keys, numeric ranges,
the probability sum, and agreement between the choice and the highest probability.
No-match existence or a winning `none` option returns `no_match`. Otherwise, the
candidate probability and margin must pass the configured thresholds. Identical
app/title/location metadata always returns `ambiguous`, even if the model reports
a concentrated distribution.

The thresholds are provisional. A model's reported confidence describes its
probability distribution; it does not measure verified accuracy on these windows.
See TypeSafe's [confidence documentation](https://docs.typesafe.ai/confidence).
Prompt instructions treat titles as data, but cannot guarantee resistance to
misleading or injected title text. TypeSafe documents these and option-order
effects in its [known failure modes](https://docs.typesafe.ai/model-jaggedness/jev-1.13).

## Revalidation

Before an action, reread state and compare window ID, PID, app, title, eligibility,
and Space membership. A changed title requires a new request. Resolve destination
Space indexes from their stable IDs after reordering. A bring operation fails if
the active Space changes during matching.

After activation or movement, reread state before focusing. swm's movement command
also checks actual native Space membership. A failed or timed-out command is
observed once and never retried automatically, because the native move may finish
later. Previous successful steps are reported when a later check fails.

These checks are separate process calls. A window or Space can still change
between the final query and command submission. Removing that gap would require
an atomic identity-checked command in swm. Current checks do not detect a reused
ID with the same PID and identical metadata, or every new competing window opened
after matching.

swm's `has-focus` window field currently reports the app's main Accessibility
window. It cannot independently establish which window is globally foreground.
The helper reports successful focus commands as `submitted`; desktop verification
remains necessary. Space activation inherits swm's fullscreen topology limits.

## Source structure

- `cmd/swm-find` handles signals and starts the CLI.
- `internal/cli` parses options, connects dependencies, and writes results.
- `internal/swm` decodes query schemas and runs bounded, timed processes.
- `internal/typesafe` builds typed questions and validates HTTP responses.
- `internal/match` filters candidates and applies selection thresholds.
- `internal/actions` prepares commands and revalidates desktop state.
- `cmd/swm-find-eval` and `internal/eval` compare labeled fixtures with a lexical baseline.
- `internal/testutil` shares synthetic state and fake backends between tests.

The model default is pinned using TypeSafe's [versioned model ID](https://docs.typesafe.ai/models).
Transport follows the [HTTP API](https://docs.typesafe.ai/api), does not follow
redirects, and does not echo upstream response bodies in errors.
