package match_test

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/testutil"
)

func TestCandidateEligibilityAndExclusion(t *testing.T) {
	snapshot := testutil.Snapshot(t)
	candidates, err := match.Candidates(snapshot, []string{"sAFaRi"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []uint32{}
	for _, candidate := range candidates {
		ids = append(ids, candidate.Window.ID)
	}
	if !slices.Equal(ids, []uint32{101, 102, 104, 105, 106}) {
		t.Fatalf("eligible IDs: %v", ids)
	}
	if candidates[0].SpaceID != 11 || candidates[0].Location != "another desktop Space" {
		t.Fatal("lost stable Space context")
	}
}

func TestResolveOutcomes(t *testing.T) {
	cases := []struct {
		name  string
		id    uint32
		alter func(*match.Prediction, []match.Candidate)
		want  string
	}{
		{"clear", 101, nil, "matched"},
		{"unsupported", 101, func(p *match.Prediction, _ []match.Candidate) { p.Exists = 0.3 }, "no_match"},
		{"none wins", 101, func(p *match.Prediction, c []match.Candidate) {
			p.Choice = "none"
			p.Probabilities[c[0].Key] = 0
			p.Probabilities["none"] = 1
		}, "no_match"},
		{"split", 101, func(p *match.Prediction, c []match.Candidate) {
			p.Probabilities[c[0].Key] = 0.55
			p.Probabilities[c[1].Key] = 0.45
		}, "ambiguous"},
		{"indistinguishable despite confidence", 105, nil, "ambiguous"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			evaluator := testutil.EvaluatorFunc(func(_ context.Context, _ string, c []match.Candidate) (match.Prediction, error) {
				p := testutil.Prediction(c, test.id)
				if test.alter != nil {
					test.alter(&p, c)
				}
				return p, nil
			})
			result, err := match.Resolve(context.Background(), "the Atlas terminal", testutil.Snapshot(t), nil, evaluator, match.DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.want {
				t.Fatalf("got %s, want %s", result.Status, test.want)
			}
			if (result.Selected != nil) != (test.want == "matched") {
				t.Fatal("non-match retained actionable selection")
			}
		})
	}
}

func TestEmptyCandidatesAvoidEvaluator(t *testing.T) {
	snapshot := testutil.Snapshot(t)
	snapshot.Windows = nil
	called := false
	evaluator := testutil.EvaluatorFunc(func(context.Context, string, []match.Candidate) (match.Prediction, error) {
		called = true
		return match.Prediction{}, nil
	})
	result, err := match.Resolve(context.Background(), "anything", snapshot, nil, evaluator, match.DefaultPolicy())
	if err != nil || result.Status != "no_match" || called {
		t.Fatalf("empty outcome %v, %v, called=%v", result, err, called)
	}
}

func TestPredictionValidation(t *testing.T) {
	candidates, err := match.Candidates(testutil.Snapshot(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*match.Prediction){
		"unknown choice":      func(p *match.Prediction) { p.Choice = "window_that_does_not_exist" },
		"missing probability": func(p *match.Prediction) { delete(p.Probabilities, "none") },
		"wrong sum":           func(p *match.Prediction) { p.Probabilities["none"] = 0.4 },
		"choice disagrees":    func(p *match.Prediction) { p.Choice = "none" },
		"NaN":                 func(p *match.Prediction) { p.Exists = math.NaN() },
		"negative":            func(p *match.Prediction) { p.Probabilities["none"] = -0.1 },
		"missing model":       func(p *match.Prediction) { p.Model = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p := testutil.Prediction(candidates, 101)
			mutate(&p)
			if err := match.ValidatePrediction(p, candidates); err == nil {
				t.Fatal("accepted invalid prediction")
			}
		})
	}
}

func TestPolicyRejectsZeroAndNonfiniteThresholds(t *testing.T) {
	for _, value := range []float64{0, -1, 1.1, math.NaN(), math.Inf(1)} {
		policy := match.DefaultPolicy()
		policy.MinMargin = value
		if policy.Validate() == nil {
			t.Fatalf("accepted %v", value)
		}
	}
}

func TestCandidateLimitDoesNotSilentlyDropWindows(t *testing.T) {
	snapshot := testutil.Snapshot(t)
	base := snapshot.Windows[1]
	snapshot.Windows = nil
	for i := range snapshot.Spaces {
		snapshot.Spaces[i].Windows = nil
	}
	for id := uint32(1); id <= 255; id++ {
		w := base
		w.ID = id
		snapshot.Windows = append(snapshot.Windows, w)
		snapshot.Spaces[0].Windows = append(snapshot.Spaces[0].Windows, id)
	}
	if _, err := match.Candidates(snapshot, nil); err == nil {
		t.Fatal("silently exceeded Choice limit")
	}
}
