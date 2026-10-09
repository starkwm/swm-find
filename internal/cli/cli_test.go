package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/starkwm/swm-find/internal/cli"
	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/testutil"
)

func TestJSONMatchAndDryRun(t *testing.T) {
	for _, args := range [][]string{{"match", "--json", "the Memoir terminal"}, {"bring", "--json", "--dry-run", "the Memoir terminal"}} {
		backend := &testutil.Backend{State: testutil.Snapshot(t)}
		runner := cli.Runner{Backend: backend, Evaluator: testutil.EvaluatorFunc(func(_ context.Context, _ string, c []match.Candidate) (match.Prediction, error) {
			return testutil.Prediction(c, 101), nil
		})}
		var out, diagnostics bytes.Buffer
		code := runner.Run(context.Background(), args, &out, &diagnostics)
		var result struct {
			Status    string `json:"status"`
			Operation string `json:"operation"`
			Action    *struct {
				Status string `json:"action_status"`
			} `json:"action"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if code != 0 || result.Status != "matched" || len(backend.Commands) != 0 || diagnostics.Len() != 0 {
			t.Fatalf("code=%d result=%v commands=%v stderr=%q", code, result, backend.Commands, diagnostics.String())
		}
		if args[0] == "bring" && (result.Action == nil || result.Action.Status != "planned") {
			t.Fatal("missing dry run")
		}
	}
}

func TestNoMatchAndAmbiguousNeverExecute(t *testing.T) {
	for _, exists := range []float64{0.1, 1} {
		backend := &testutil.Backend{State: testutil.Snapshot(t)}
		runner := cli.Runner{Backend: backend, Evaluator: testutil.EvaluatorFunc(func(_ context.Context, _ string, c []match.Candidate) (match.Prediction, error) {
			p := testutil.Prediction(c, 105)
			p.Exists = exists
			return p, nil
		})}
		var out, diagnostics bytes.Buffer
		code := runner.Run(context.Background(), []string{"focus", "--json", "a terminal"}, &out, &diagnostics)
		if code != 2 || len(backend.Commands) != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("code=%d commands=%v output=%s", code, backend.Commands, out.String())
		}
	}
}

func TestInvalidArgumentsDoNotReadDesktop(t *testing.T) {
	for _, args := range [][]string{{"other"}, {"match"}, {"match", "--min-probability", "NaN", "query"}, {"match", "--api-timeout", "0s", "query"}, {"match", "--dry-run", "query"}} {
		backend := &testutil.Backend{State: testutil.Snapshot(t)}
		var out, diagnostics bytes.Buffer
		code := (cli.Runner{Backend: backend}).Run(context.Background(), args, &out, &diagnostics)
		if code != 64 || backend.SnapshotCount != 0 {
			t.Fatalf("args=%v code=%d reads=%d", args, code, backend.SnapshotCount)
		}
	}
}

func TestAPIErrorProducesJSONAndPreventsAction(t *testing.T) {
	backend := &testutil.Backend{State: testutil.Snapshot(t)}
	runner := cli.Runner{Backend: backend, Evaluator: testutil.EvaluatorFunc(func(context.Context, string, []match.Candidate) (match.Prediction, error) {
		return match.Prediction{}, errors.New("service unavailable")
	})}
	var out, diagnostics bytes.Buffer
	code := runner.Run(context.Background(), []string{"bring", "--json", "query"}, &out, &diagnostics)
	if code != 1 || len(backend.Commands) != 0 || !strings.Contains(out.String(), `"status": "error"`) || !json.Valid(out.Bytes()) {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
}

func TestExcludeAppliedBeforeEvaluator(t *testing.T) {
	backend := &testutil.Backend{State: testutil.Snapshot(t)}
	evaluator := testutil.EvaluatorFunc(func(_ context.Context, _ string, c []match.Candidate) (match.Prediction, error) {
		for _, candidate := range c {
			if candidate.Window.App == "Safari" {
				t.Fatal("excluded metadata reached evaluator")
			}
		}
		return testutil.Prediction(c, 101), nil
	})
	var out, diagnostics bytes.Buffer
	code := (cli.Runner{Backend: backend, Evaluator: evaluator}).Run(context.Background(), []string{"match", "--exclude-app", "safari", "query"}, &out, &diagnostics)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, diagnostics.String())
	}
}

func TestHumanOutputEscapesControlCharacters(t *testing.T) {
	backend := &testutil.Backend{State: testutil.Snapshot(t)}
	backend.State.Windows[0].Title = "Memoir\n\x1b[2J"
	evaluator := testutil.EvaluatorFunc(func(_ context.Context, _ string, c []match.Candidate) (match.Prediction, error) {
		return testutil.Prediction(c, 101), nil
	})
	var out, diagnostics bytes.Buffer
	code := (cli.Runner{Backend: backend, Evaluator: evaluator}).Run(context.Background(), []string{"match", "query"}, &out, &diagnostics)
	if code != 0 || strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), `\n\x1b`) {
		t.Fatalf("unescaped output %q", out.String())
	}
}
