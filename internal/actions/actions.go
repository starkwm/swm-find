// Package actions revalidates a selection before submitting fixed swm operations.
package actions

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/swm"
)

// ErrStale indicates the desktop state changed before an action.
var ErrStale = errors.New("desktop context changed; run the request again")

// Step records one submitted swm operation.
type Step struct {
	Arguments []string `json:"arguments"`
	Status    string   `json:"status"`
}

// Result summarizes an action attempt.
type Result struct {
	Status             string  `json:"action_status"`
	DestinationSpaceID uint64  `json:"destination_space_id"`
	Steps              []Step  `json:"steps"`
	ObservedSpaceID    *uint64 `json:"observed_space_id,omitempty"`
}

// Run revalidates and executes a focus or bring operation.
func Run(ctx context.Context, backend swm.Backend, operation string, selected match.Candidate, initial swm.Snapshot, dryRun bool) (Result, error) {
	result := Result{Status: "planned", Steps: []Step{}}
	if operation != "focus" && operation != "bring" {
		return result, errors.New("unknown window operation")
	}

	destination, ok := initial.SpaceByID(selected.SpaceID)
	if !ok {
		return result, ErrStale
	}

	if operation == "bring" {
		var err error
		destination, err = initial.ActiveSpace()
		if err != nil {
			return result, err
		}
	}

	result.DestinationSpaceID = destination.ID
	expectedSpaceID := selected.SpaceID

	preflight := func() (swm.Snapshot, swm.Space, error) {
		current, err := backend.Snapshot(ctx)
		if err != nil {
			return current, swm.Space{}, err
		}

		if err := current.Validate(); err != nil {
			return current, swm.Space{}, err
		}

		window, found := current.WindowByID(selected.Window.ID)
		if !found || !sameIdentity(window, selected.Window) || !match.Eligible(window) {
			return current, swm.Space{}, ErrStale
		}

		space, err := current.WindowSpace(window)
		if err != nil || space.ID != expectedSpaceID {
			return current, swm.Space{}, ErrStale
		}

		target, found := current.SpaceByID(destination.ID)
		if !found || !target.Normal() {
			return current, swm.Space{}, ErrStale
		}

		if operation == "bring" {
			active, err := current.ActiveSpace()
			if err != nil || active.ID != destination.ID {
				return current, swm.Space{}, ErrStale
			}

			if expectedSpaceID != destination.ID && (window.CanMove == nil || !*window.CanMove) {
				return current, swm.Space{}, errors.New("selected window cannot move")
			}
		}

		return current, target, nil
	}

	fail := func(err error) (Result, error) {
		if errors.Is(err, ErrStale) {
			result.Status = "stale_context"
		} else {
			result.Status = "failed"
		}

		for _, step := range result.Steps {
			if step.Status == "submitted" || step.Status == "confirmed" {
				result.Status = "partial"
			}
		}

		return result, err
	}

	submit := func(args []string, movement bool) error {
		result.Steps = append(result.Steps, Step{Arguments: args, Status: "submitted"})

		if err := backend.Execute(ctx, args...); err != nil {
			result.Steps[len(result.Steps)-1].Status = "unconfirmed"
			result.Status = "unconfirmed"

			// A timed-out native operation can complete later. Observe once; never resubmit.
			if current, readErr := backend.Snapshot(ctx); readErr == nil {
				if w, found := current.WindowByID(selected.Window.ID); found && sameIdentity(w, selected.Window) {
					if space, spaceErr := current.WindowSpace(w); spaceErr == nil {
						result.ObservedSpaceID = &space.ID
					}
				}
			}

			return fmt.Errorf("swm operation was not confirmed; inspect state before retrying: %w", err)
		}

		if movement {
			result.Steps[len(result.Steps)-1].Status = "confirmed"
		}

		return nil
	}

	current, target, err := preflight()
	if err != nil {
		return fail(err)
	}

	if operation == "focus" && !target.IsVisible {
		if err := current.CanActivate(target); err != nil {
			return fail(err)
		}

		args := []string{"space", "activate", strconv.Itoa(target.Index)}
		if dryRun {
			result.Steps = append(result.Steps, Step{args, "planned"})
		} else {
			if err := submit(args, true); err != nil {
				return result, err
			}

			_, target, err = preflight()
			if err != nil {
				return fail(err)
			}

			if !target.IsVisible {
				return fail(errors.New("target Space did not become visible"))
			}
		}
	}

	if operation == "bring" && expectedSpaceID != destination.ID {
		args := []string{"window", "space", "--window", swm.WindowID(selected.Window.ID), strconv.Itoa(target.Index)}
		if dryRun {
			result.Steps = append(result.Steps, Step{args, "planned"})
		} else {
			if err := submit(args, true); err != nil {
				return result, err
			}

			expectedSpaceID = destination.ID

			_, _, err = preflight()
			if err != nil {
				return fail(err)
			}

			result.ObservedSpaceID = &destination.ID
		}
	}

	args := []string{"window", "focus", "--window", swm.WindowID(selected.Window.ID)}
	if dryRun {
		result.Steps = append(result.Steps, Step{args, "planned"})
		return result, nil
	}

	if err := submit(args, false); err != nil {
		return result, err
	}

	// swm's has-focus is an app's main-window flag, not independent foreground proof.
	result.Status = "submitted"

	return result, nil
}

func sameIdentity(a, b swm.Window) bool {
	return a.ID == b.ID && a.PID != nil && b.PID != nil && *a.PID == *b.PID && a.App == b.App && a.Title == b.Title
}
