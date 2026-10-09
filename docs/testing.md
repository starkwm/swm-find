# Evaluation and desktop verification

## Automated checks

```sh
make check
make build
```

Tests cover candidate eligibility, exclusion, uncertain decisions, malformed
responses, missing keys, HTTP failures, request cancellation, redirects, process
timeouts, argument arrays, stale window identity, Space reordering, unsupported
fullscreen topology, dry runs, partial operations, and late movement observations.

The HTTP tests bind local loopback ports. A sandbox that blocks listening sockets
needs permission for those tests. No test requires a real TypeSafe account or a
running swm daemon.

## Labeled evaluation

`testdata/snapshot.json` contains synthetic windows. `testdata/cases.json` has
21 labeled descriptions, with eight training cases and thirteen held-out cases.
The labels are expected behavior, not measured Jev results.

Run the local lexical baseline without a key or network access:

```sh
bin/swm-find-eval
bin/swm-find-eval --split all
```

Run Jev explicitly using only the supplied synthetic metadata:

```sh
bin/swm-find-eval --jev --split train
bin/swm-find-eval --jev --split holdout
bin/swm-find-eval --jev --split holdout --seed 17
```

The tool never queries live windows. Custom `--snapshot` and `--cases` paths are
supported; `--jev` sends those files' descriptions and candidate metadata to
TypeSafe. It reports per-case decisions, correct counts, request errors, actual
model versions, and selection p50/p95 latency. Incorrect decisions are included
in the report; request or fixture errors return a nonzero exit.

The lexical baseline requires each meaningful description token in the app,
title, or relative Space location. It supplies no probability estimates and
supports no semantic app aliases. Compare useful-match accuracy, false matches,
and ambiguity refusal separately. Tune thresholds on training cases, then assess
held-out cases without further adjustment. Expand the synthetic set with sanitized
examples reflecting your apps and titles. Repeat with several seeds to inspect
option-order sensitivity.

## Desktop checks

Start with a running, compatible swm daemon and descriptive app titles. Run
`match --json` on requests whose intended window you can identify yourself. Check
the exact selected ID against `swm query windows`. A missing or ambiguous result
should leave the desktop unchanged.

Next, inspect dry runs:

```sh
bin/swm-find focus --dry-run 'the Atlas terminal'
bin/swm-find bring --dry-run 'the Atlas terminal'
```

Once the selected window and destination are correct, run the same operation
without `--dry-run`. Verify focus visually. Verify movement through both the
desktop and `swm query spaces`. Do not infer global focus from `has-focus`.

Exercise hidden Spaces, multiple displays, shared versus separate display Spaces,
duplicate window titles, excluded apps, minimized windows, and unsupported native
fullscreen layouts. Close or retitle a window while matching, change the active
Space during bring, and reorder Spaces to check stale-context handling.

After an `unconfirmed` result, query the current state before deciding to retry.
An earlier submitted move may complete after the command returned. Selection
latency and swm's native movement/activation time should be recorded separately.

## Initial verification limits

Creating this project validates the Go code and mocked contracts. Live Jev
quality, real account access, network latency, native focus, and desktop movement
need the evaluation and manual steps above. Automated tests do not establish
those results.
