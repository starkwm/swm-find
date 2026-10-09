# swm-find

Find and control macOS windows using natural-language descriptions.

`swm-find` reads window metadata from [swm](https://github.com/starkwm/swm), asks
TypeSafe's Jev model to select a window, and uses existing swm commands to focus it
or bring it to the current desktop Space. Matching returns an ambiguous or missing
result when it cannot establish one supported target.

## Quick start

Requires Go 1.27 to build, a running swm daemon on a supported macOS version, and
a TypeSafe API key. The project has no third-party Go dependencies.

```sh
make build
export TYPESAFE_API_KEY='your-api-key'
bin/swm-find match --json 'the Memoir terminal'
bin/swm-find bring --dry-run 'the Memoir terminal'
```

Jev receives your description and eligible window app names, titles, and relative
Space locations. Exclude apps with repeated `--exclude-app` options. The helper
does not save request logs or title logs. JSON and terminal results include titles
so you can inspect which window matched.

The default model is pinned to `jev-1.13.0`. Override it with `--model` or
`SWM_FIND_MODEL`. The initial probability thresholds are starting values. Evaluate
them on representative snapshots before relying on automatic window actions.

## Development

```sh
make check
make build
bin/swm-find-eval
```

Checks use synthetic snapshots, fake swm processes, and local HTTP servers. They
do not require a TypeSafe key or move live windows. The evaluation tool defaults
to a local lexical baseline on held-out synthetic descriptions. Add `--jev` to
evaluate the same descriptions with a live TypeSafe account.

## Documentation

- [Command line and JSON results](docs/cli.md)
- [Matching, architecture, and limits](docs/design.md)
- [Evaluation and desktop verification](docs/testing.md)

The project uses the BSD 3-Clause license, matching swm.
