// Package eval compares Jev matching with a local lexical baseline on labeled snapshots.
package eval

import (
	"context"
	"math/rand"
	"strings"
	"unicode"

	"github.com/starkwm/swm-find/internal/match"
)

// Baseline requires every meaningful token in app/title/location metadata.
// Its output is a decision, not a calibrated probability estimate.
func Baseline(query string, candidates []match.Candidate) (string, uint32) {
	ignored := map[string]bool{"the": true, "a": true, "an": true, "my": true, "for": true, "with": true, "open": true, "on": true, "working": true, "window": true}
	tokens := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var matches []uint32
	for _, candidate := range candidates {
		text := strings.ToLower(candidate.Window.App + " " + candidate.Window.Title + " " + candidate.Location)
		supported, meaningful := true, false
		for _, token := range tokens {
			if ignored[token] {
				continue
			}
			meaningful = true
			if !strings.Contains(text, token) {
				supported = false
				break
			}
		}
		if meaningful && supported {
			matches = append(matches, candidate.Window.ID)
		}
	}
	if len(matches) == 1 {
		return "matched", matches[0]
	}
	if len(matches) > 1 {
		return "ambiguous", 0
	}
	return "no_match", 0
}

// Shuffled varies option order while retaining the original window identity mapping.
type Shuffled struct {
	Evaluator match.Evaluator
	Seed      int64
}

// Evaluate restores the original candidate keys after model evaluation.
func (s Shuffled) Evaluate(ctx context.Context, query string, candidates []match.Candidate) (match.Prediction, error) {
	shuffled := append([]match.Candidate(nil), candidates...)
	rng := rand.New(rand.NewSource(s.Seed))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	keyMap := map[string]string{"none": "none"}
	for i := range shuffled {
		newKey := candidates[i].Key
		keyMap[newKey] = shuffled[i].Key
		shuffled[i].Key = newKey
	}
	prediction, err := s.Evaluator.Evaluate(ctx, query, shuffled)
	if err != nil {
		return prediction, err
	}
	if err := match.ValidatePrediction(prediction, shuffled); err != nil {
		return prediction, err
	}
	prediction.Choice = keyMap[prediction.Choice]
	probabilities := make(map[string]float64, len(prediction.Probabilities))
	for key, value := range prediction.Probabilities {
		probabilities[keyMap[key]] = value
	}
	prediction.Probabilities = probabilities
	return prediction, nil
}
