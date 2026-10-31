// Package match prepares window candidates and applies explicit acceptance thresholds.
package match

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/starkwm/swm-find/internal/swm"
)

// MaxCandidates is the largest number of windows that can be evaluated in one request.
const MaxCandidates = 254

// MaxDescriptionBytes is the maximum accepted query length.
const MaxDescriptionBytes = 4096

// Candidate represents one eligible window exposed to the model.
type Candidate struct {
	Key         string     `json:"key"`
	Window      swm.Window `json:"window"`
	SpaceID     uint64     `json:"space_id"`
	Location    string     `json:"location"`
	Probability float64    `json:"probability"`
}

// Prediction is a model response for a window-choice request.
type Prediction struct {
	Model         string
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
	Exists        float64
}

// Evaluator asks the model to choose among candidates.
type Evaluator interface {
	Evaluate(context.Context, string, []Candidate) (Prediction, error)
}

// Policy holds acceptance thresholds for a match result.
type Policy struct {
	MinProbability float64 `json:"min_probability"`
	MinExists      float64 `json:"min_exists"`
	MinMargin      float64 `json:"min_margin"`
}

// DefaultPolicy returns the initial acceptance thresholds.
func DefaultPolicy() Policy { return Policy{0.8, 0.85, 0.2} }

// Validate reports whether every threshold is a nonzero probability.
func (p Policy) Validate() error {
	for _, value := range []float64{p.MinProbability, p.MinExists, p.MinMargin} {
		if !probability(value) || value == 0 {
			return errors.New("thresholds must be greater than zero and at most one")
		}
	}

	return nil
}

// Result is the outcome of resolving a description against a snapshot.
type Result struct {
	Status      string      `json:"status"`
	Selected    *Candidate  `json:"selected,omitempty"`
	Candidates  []Candidate `json:"candidates"`
	Model       string      `json:"model,omitempty"`
	Exists      float64     `json:"exists_probability"`
	Confidence  float64     `json:"confidence"`
	Margin      float64     `json:"margin"`
	SelectionMS int64       `json:"selection_ms"`
	Policy      Policy      `json:"policy"`
}

// Eligible reports whether a window may be considered as a match target.
func Eligible(w swm.Window) bool {
	return w.ID != 0 && w.PID != nil && *w.PID > 0 && w.App != "" && w.HasAXReference &&
		!w.IsMinimized && !w.IsNativeFullscreen && w.Role == "AXWindow" &&
		w.Subrole == "AXStandardWindow" && w.Layer != nil && *w.Layer == 0
}

// Candidates builds the eligible, excluded, and Space-annotated candidate list.
func Candidates(snapshot swm.Snapshot, excluded []string) ([]Candidate, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}

	windows := append([]swm.Window(nil), snapshot.Windows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].ID < windows[j].ID })

	result := make([]Candidate, 0, len(windows))
	for _, w := range windows {
		if !Eligible(w) || isExcluded(w.App, excluded) {
			continue
		}

		space, err := snapshot.WindowSpace(w)
		if err != nil {
			continue
		}

		location := "another desktop Space"
		if space.HasFocus {
			location = "the current desktop Space"
		}

		result = append(result, Candidate{Key: fmt.Sprintf("w%d", len(result)+1), Window: w, SpaceID: space.ID, Location: location})
	}

	if len(result) > MaxCandidates {
		return nil, fmt.Errorf("more than %d eligible windows; exclude apps to narrow the candidates", MaxCandidates)
	}

	return result, nil
}

func isExcluded(app string, excluded []string) bool {
	for _, value := range excluded {
		if strings.EqualFold(app, value) {
			return true
		}
	}

	return false
}

// Resolve asks the model to select a candidate and applies the policy.
func Resolve(ctx context.Context, query string, snapshot swm.Snapshot, excluded []string, evaluator Evaluator, policy Policy) (Result, error) {
	if err := policy.Validate(); err != nil {
		return Result{}, err
	}

	if strings.TrimSpace(query) == "" || len(query) > MaxDescriptionBytes {
		return Result{}, fmt.Errorf("description must contain 1 to %d bytes", MaxDescriptionBytes)
	}

	candidates, err := Candidates(snapshot, excluded)
	if err != nil {
		return Result{}, err
	}

	result := Result{Status: "no_match", Candidates: candidates, Policy: policy}
	if len(candidates) == 0 {
		return result, nil
	}

	start := time.Now()
	prediction, err := evaluator.Evaluate(ctx, query, candidates)
	result.SelectionMS = time.Since(start).Milliseconds()
	if err != nil {
		return result, err
	}

	if err := ValidatePrediction(prediction, candidates); err != nil {
		return result, err
	}

	result.Model, result.Exists, result.Confidence = prediction.Model, prediction.Exists, prediction.Confidence
	for i := range candidates {
		candidates[i].Probability = prediction.Probabilities[candidates[i].Key]
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Probability > candidates[j].Probability })
	result.Candidates = candidates

	if prediction.Choice == "none" || prediction.Exists < policy.MinExists {
		return result, nil
	}

	winner := candidates[0]
	runnerUp := prediction.Probabilities["none"]
	if len(candidates) > 1 {
		runnerUp = math.Max(runnerUp, candidates[1].Probability)
	}

	result.Margin = winner.Probability - runnerUp
	result.Status = "ambiguous"
	if winner.Probability < policy.MinProbability || result.Margin < policy.MinMargin {
		return result, nil
	}

	// Indistinguishable metadata cannot establish which physical window was intended.
	for _, other := range candidates[1:] {
		if strings.EqualFold(other.Window.App, winner.Window.App) && other.Window.Title == winner.Window.Title && other.Location == winner.Location {
			return result, nil
		}
	}

	result.Status, result.Selected = "matched", &winner

	return result, nil
}

// ValidatePrediction checks that a model response has a complete, consistent shape.
func ValidatePrediction(p Prediction, candidates []Candidate) error {
	if p.Model == "" || !probability(p.Confidence) || !probability(p.Exists) || len(p.Probabilities) != len(candidates)+1 {
		return errors.New("TypeSafe returned incomplete or invalid prediction fields")
	}

	keys := map[string]bool{"none": true}
	for _, candidate := range candidates {
		keys[candidate.Key] = true
	}

	if !keys[p.Choice] {
		return errors.New("TypeSafe selected an unknown candidate")
	}

	sum := 0.0
	for key, value := range p.Probabilities {
		if !keys[key] || !probability(value) {
			return errors.New("TypeSafe returned an invalid probability distribution")
		}

		if value > p.Probabilities[p.Choice]+1e-6 {
			return errors.New("TypeSafe choice does not match the highest probability")
		}

		sum += value
	}

	if math.Abs(sum-1) > 1e-5 {
		return errors.New("TypeSafe probabilities do not sum to one")
	}

	return nil
}

func probability(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
