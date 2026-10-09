// Package testutil shares synthetic snapshots across integration tests.
package testutil

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/swm"
)

func Snapshot(t testing.TB) swm.Snapshot {
	t.Helper()
	data, err := os.ReadFile("../../testdata/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot swm.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func Clone(snapshot swm.Snapshot) swm.Snapshot {
	data, _ := json.Marshal(snapshot)
	var result swm.Snapshot
	_ = json.Unmarshal(data, &result)
	return result
}

type EvaluatorFunc func(context.Context, string, []match.Candidate) (match.Prediction, error)

func (f EvaluatorFunc) Evaluate(ctx context.Context, query string, candidates []match.Candidate) (match.Prediction, error) {
	return f(ctx, query, candidates)
}

func Prediction(candidates []match.Candidate, id uint32) match.Prediction {
	prediction := match.Prediction{Model: "fixture-model", Probabilities: map[string]float64{"none": 0}, Confidence: 1, Exists: 1}
	for _, candidate := range candidates {
		prediction.Probabilities[candidate.Key] = 0
		if candidate.Window.ID == id {
			prediction.Choice = candidate.Key
			prediction.Probabilities[candidate.Key] = 1
		}
	}
	return prediction
}

type Backend struct {
	State         swm.Snapshot
	SnapshotCount int
	Commands      [][]string
	OnSnapshot    func(*Backend)
	OnExecute     func(*Backend, []string) error
}

func (b *Backend) Snapshot(context.Context) (swm.Snapshot, error) {
	b.SnapshotCount++
	if b.OnSnapshot != nil {
		b.OnSnapshot(b)
	}
	return Clone(b.State), nil
}

func (b *Backend) Execute(_ context.Context, args ...string) error {
	b.Commands = append(b.Commands, append([]string(nil), args...))
	if b.OnExecute != nil {
		return b.OnExecute(b, args)
	}
	return nil
}
