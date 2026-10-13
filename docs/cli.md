# Command line

```sh
swm-find match [options] <window description>
swm-find focus [options] <window description>
swm-find bring [options] <window description>
```

Place options before the description. Quote descriptions containing shell
characters. Descriptions select windows; the subcommand determines the operation.

`match` reports candidates without changing the desktop. `focus` activates the
target window's normal desktop Space when it is hidden, then focuses the window.
`bring` captures the active normal desktop Space before matching, moves the window
there, and focuses it. In a shared Space across multiple displays, bringing a
window does not promise placement on the pointer's display.

## Options

| Option              | Default      | Meaning                                                  |
| ------------------- | ------------ | -------------------------------------------------------- |
| `--json`            | false        | Write machine-readable results to stdout.                |
| `--dry-run`         | false        | Show focus or bring commands without submitting them.    |
| `--swm`             | `swm`        | Path to the swm executable.                              |
| `--model`           | `jev-1.13.0` | TypeSafe model ID; `SWM_FIND_MODEL` changes the default. |
| `--api-timeout`     | `2s`         | Deadline for matching.                                   |
| `--swm-timeout`     | `5s`         | Deadline for each swm process.                           |
| `--exclude-app`     | none         | Exact app name to omit, ignoring case. Repeat as needed. |
| `--min-probability` | `0.8`        | Required probability for the selected candidate.         |
| `--min-exists`      | `0.85`       | Required probability that a supported match exists.      |
| `--min-margin`      | `0.2`        | Required lead over the runner-up or the `none` option.   |

All thresholds must be greater than zero and at most one. `match` already leaves
the desktop unchanged and does not accept `--dry-run`. `--help` prints usage;
`--version` currently reports a development build.

## JSON results

The top-level object contains `status`, `operation`, `dry_run`, `selected`,
`candidates`, the actual model version, probability fields, `selection_ms`, and
the applied `policy`. `selected` is present only for a `matched` result. Candidates
include the local window metadata, stable `space_id`, and their probabilities.

Selection statuses are `matched`, `ambiguous`, `no_match`, and `error`. A focus or
bring result also contains an `action` object with the destination Space ID,
submitted argument arrays, and step statuses.

| Action status   | Meaning                                                                         |
| --------------- | ------------------------------------------------------------------------------- |
| `planned`       | Dry run; no desktop command was executed.                                       |
| `submitted`     | swm accepted the operations. Foreground focus was not independently verified.   |
| `stale_context` | Window identity, metadata, membership, or destination changed before a command. |
| `partial`       | An earlier step succeeded, then a later check failed.                           |
| `unconfirmed`   | A command failed or timed out after submission; effects may occur later.        |
| `failed`        | Preparation failed before a command was submitted.                              |

An action error does not erase a valid selection. Check both the selection status
and action status, and treat a nonzero exit as an unsuccessful operation. When
available, `observed_space_id` reports membership after a move or failed command.
It is an observation, not a promise that a delayed move has finished.

## Exit codes

| Code | Meaning                                                                    |
| ---- | -------------------------------------------------------------------------- |
| `0`  | Matched, submitted, or a successful dry run.                               |
| `1`  | swm/API failure, invalid response, stale context, or an incomplete action. |
| `2`  | Ambiguous or missing match; no desktop commands were executed.             |
| `64` | Invalid command-line arguments.                                            |

JSON mode writes an error field to stdout for runtime failures. Usage errors go
to stderr. Human-readable output escapes title control characters and shows up to
five candidate windows.
