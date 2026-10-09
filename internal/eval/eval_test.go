package eval_test

import (
	"context"
	"testing"

	"github.com/starkwm/swm-find/internal/eval"
	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/testutil"
)

func TestLexicalBaseline(t *testing.T) {
	candidates, _ := match.Candidates(testutil.Snapshot(t), nil)
	for _, test := range []struct {
		query, status string
		id            uint32
	}{{"Memoir", "matched", 101}, {"Conjuror", "ambiguous", 0}, {"Slack", "no_match", 0}, {"the window", "no_match", 0}} {
		status, id := eval.Baseline(test.query, candidates)
		if status != test.status || id != test.id {
			t.Fatalf("%q: %s/%d", test.query, status, id)
		}
	}
}

func TestShufflePreservesWindowIdentity(t *testing.T) {
	candidates, _ := match.Candidates(testutil.Snapshot(t), nil)
	evaluator := testutil.EvaluatorFunc(func(_ context.Context, _ string, c []match.Candidate) (match.Prediction, error) {
		return testutil.Prediction(c, 101), nil
	})
	for seed := int64(0); seed < 20; seed++ {
		prediction, err := (eval.Shuffled{Evaluator: evaluator, Seed: seed}).Evaluate(context.Background(), "Memoir", candidates)
		if err != nil || prediction.Choice != candidates[0].Key || prediction.Probabilities[candidates[0].Key] != 1 {
			t.Fatalf("seed=%d prediction=%v err=%v", seed, prediction, err)
		}
	}
}
