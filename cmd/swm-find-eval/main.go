// swm-find-eval evaluates labeled synthetic snapshots; it never calls swm.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/starkwm/swm-find/internal/eval"
	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/swm"
	"github.com/starkwm/swm-find/internal/typesafe"
)

type testCase struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	WindowID    uint32 `json:"window_id"`
	Split       string `json:"split"`
}

type outcome struct {
	Status   string `json:"status"`
	WindowID uint32 `json:"window_id,omitempty"`
	Correct  bool   `json:"correct"`
}

type row struct {
	Case        testCase `json:"case"`
	Baseline    outcome  `json:"baseline"`
	Jev         *outcome `json:"jev,omitempty"`
	SelectionMS int64    `json:"selection_ms,omitempty"`
	Model       string   `json:"model,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func main() { os.Exit(run()) }

func run() int {
	snapshotPath := flag.String("snapshot", "testdata/snapshot.json", "Labeled snapshot file; never queries live windows.")
	casesPath := flag.String("cases", "testdata/cases.json", "Labeled descriptions file.")
	split := flag.String("split", "holdout", "train, holdout, or all.")
	jev := flag.Bool("jev", false, "Send the supplied fixture descriptions and window metadata to TypeSafe.")
	model := flag.String("model", typesafe.DefaultModel, "Pinned TypeSafe model ID.")
	seed := flag.Int64("seed", 0, "Seed for candidate option permutation.")
	timeout := flag.Duration("timeout", 2*time.Second, "Deadline per Jev request.")

	policy := match.DefaultPolicy()
	flag.Float64Var(&policy.MinProbability, "min-probability", policy.MinProbability, "Minimum candidate probability.")
	flag.Float64Var(&policy.MinExists, "min-exists", policy.MinExists, "Minimum match-exists probability.")
	flag.Float64Var(&policy.MinMargin, "min-margin", policy.MinMargin, "Minimum candidate lead.")
	flag.Parse()

	if *timeout <= 0 || (*split != "train" && *split != "holdout" && *split != "all") || policy.Validate() != nil {
		fmt.Fprintln(os.Stderr, "invalid timeout, split, or thresholds")
		return 64
	}

	if *jev && os.Getenv("TYPESAFE_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "set TYPESAFE_API_KEY to evaluate Jev")
		return 1
	}

	var snapshot swm.Snapshot
	var cases []testCase
	for _, file := range []struct {
		path  string
		value any
	}{{*snapshotPath, &snapshot}, {*casesPath, &cases}} {
		data, err := os.ReadFile(file.path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		if err := json.Unmarshal(data, file.value); err != nil {
			fmt.Fprintln(os.Stderr, "invalid fixture JSON:", file.path)
			return 1
		}
	}

	candidates, err := match.Candidates(snapshot, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := typesafe.Client{APIKey: os.Getenv("TYPESAFE_API_KEY"), Model: *model}
	rows := []row{}
	latencies := []int64{}
	baselineCorrect, jevCorrect, errorsCount := 0, 0, 0

	for _, test := range cases {
		if *split != "all" && test.Split != *split {
			continue
		}

		if (test.Status != "matched" && test.Status != "ambiguous" && test.Status != "no_match") || (test.Status == "matched") != (test.WindowID != 0) {
			fmt.Fprintln(os.Stderr, "invalid expected fixture outcome")
			return 1
		}

		status, id := eval.Baseline(test.Description, candidates)
		item := row{Case: test, Baseline: outcome{status, id, status == test.Status && id == test.WindowID}}
		if item.Baseline.Correct {
			baselineCorrect++
		}

		if *jev {
			requestCtx, cancel := context.WithTimeout(ctx, *timeout)
			result, requestErr := match.Resolve(requestCtx, test.Description, snapshot, nil, eval.Shuffled{Evaluator: client, Seed: *seed}, policy)
			cancel()

			if requestErr != nil {
				item.Error = requestErr.Error()
				errorsCount++
			} else {
				id = 0
				if result.Selected != nil {
					id = result.Selected.Window.ID
				}

				item.Jev = &outcome{result.Status, id, result.Status == test.Status && id == test.WindowID}
				item.SelectionMS, item.Model = result.SelectionMS, result.Model
				latencies = append(latencies, result.SelectionMS)

				if item.Jev.Correct {
					jevCorrect++
				}
			}
		}

		rows = append(rows, item)

		if ctx.Err() != nil {
			break
		}
	}

	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "no cases selected")
		return 1
	}

	result := map[string]any{"split": *split, "seed": *seed, "cases": len(rows), "baseline_correct": baselineCorrect, "results": rows}
	if *jev {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		result["jev_correct"], result["request_errors"] = jevCorrect, errorsCount

		if len(latencies) > 0 {
			result["p50_ms"] = latencies[(len(latencies)-1)/2]
			result["p95_ms"] = latencies[(len(latencies)*95+99)/100-1]
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(result) != nil {
		return 1
	}

	if errorsCount > 0 || ctx.Err() != nil {
		return 1
	}

	return 0
}
