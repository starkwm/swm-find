package actions_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/starkwm/swm-find/internal/actions"
	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/swm"
	"github.com/starkwm/swm-find/internal/testutil"
)

func selected(t *testing.T, snapshot swm.Snapshot, id uint32) match.Candidate {
	t.Helper()
	candidates, err := match.Candidates(snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.Window.ID == id {
			return candidate
		}
	}
	t.Fatalf("missing fixture window %d", id)
	return match.Candidate{}
}

func applyCommands(b *testutil.Backend, args []string) error {
	if args[0] == "space" {
		index, _ := strconv.Atoi(args[2])
		for i := range b.State.Spaces {
			space := &b.State.Spaces[i]
			space.HasFocus = space.Index == index
			if space.Displays[0] == 1 {
				space.IsVisible = space.Index == index
			}
		}
	}
	if args[0] == "window" && args[1] == "space" {
		index, _ := strconv.Atoi(args[4])
		id, _ := strconv.ParseUint(args[3], 10, 32)
		for i := range b.State.Windows {
			if b.State.Windows[i].ID == uint32(id) {
				b.State.Windows[i].Space = &index
			}
		}
		for i := range b.State.Spaces {
			space := &b.State.Spaces[i]
			kept := []uint32{}
			for _, windowID := range space.Windows {
				if windowID != uint32(id) {
					kept = append(kept, windowID)
				}
			}
			if space.Index == index {
				kept = append(kept, uint32(id))
			}
			space.Windows = kept
		}
	}
	return nil
}

func TestFocusActivatesHiddenSpaceThenTargetsWindow(t *testing.T) {
	initial := testutil.Snapshot(t)
	backend := &testutil.Backend{State: testutil.Clone(initial), OnExecute: applyCommands}
	result, err := actions.Run(context.Background(), backend, "focus", selected(t, initial, 101), initial, false)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"space", "activate", "1"}, {"window", "focus", "--window", "101"}}
	if !reflect.DeepEqual(backend.Commands, want) {
		t.Fatalf("commands=%v", backend.Commands)
	}
	if result.Status != "submitted" {
		t.Fatalf("claimed unverified foreground focus: %s", result.Status)
	}
}

func TestBringVerifiesMoveThenFocuses(t *testing.T) {
	initial := testutil.Snapshot(t)
	backend := &testutil.Backend{State: testutil.Clone(initial), OnExecute: applyCommands}
	result, err := actions.Run(context.Background(), backend, "bring", selected(t, initial, 101), initial, false)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"window", "space", "--window", "101", "0"}, {"window", "focus", "--window", "101"}}
	if !reflect.DeepEqual(backend.Commands, want) || result.ObservedSpaceID == nil || *result.ObservedSpaceID != 10 {
		t.Fatalf("result=%v commands=%v", result, backend.Commands)
	}
}

func TestDryRunDoesNotExecute(t *testing.T) {
	for _, operation := range []string{"focus", "bring"} {
		t.Run(operation, func(t *testing.T) {
			initial := testutil.Snapshot(t)
			backend := &testutil.Backend{State: testutil.Clone(initial)}
			result, err := actions.Run(context.Background(), backend, operation, selected(t, initial, 101), initial, true)
			if err != nil || result.Status != "planned" || len(result.Steps) != 2 || len(backend.Commands) != 0 {
				t.Fatalf("result=%v err=%v commands=%v", result, err, backend.Commands)
			}
		})
	}
}

func TestStaleWindowAndDestinationPreventActions(t *testing.T) {
	mutations := map[string]func(*swm.Snapshot){
		"closed":               func(s *swm.Snapshot) { s.Windows = s.Windows[1:] },
		"reused ID":            func(s *swm.Snapshot) { pid := 999; s.Windows[0].PID = &pid },
		"title changed":        func(s *swm.Snapshot) { s.Windows[0].Title = "Other project" },
		"minimized":            func(s *swm.Snapshot) { s.Windows[0].IsMinimized = true },
		"active Space changed": func(s *swm.Snapshot) { s.Spaces[0].HasFocus = false; s.Spaces[1].HasFocus = true },
		"destination deleted":  func(s *swm.Snapshot) { s.Spaces = s.Spaces[1:] },
		"source moved":         func(s *swm.Snapshot) { s.Spaces[1].Windows = []uint32{104, 107} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			initial := testutil.Snapshot(t)
			backend := &testutil.Backend{State: testutil.Clone(initial)}
			mutate(&backend.State)
			result, err := actions.Run(context.Background(), backend, "bring", selected(t, initial, 101), initial, false)
			if !errors.Is(err, actions.ErrStale) || result.Status != "stale_context" || len(backend.Commands) != 0 {
				t.Fatalf("result=%v err=%v commands=%v", result, err, backend.Commands)
			}
		})
	}
}

func TestSpaceReorderingUsesCurrentIndexForStableID(t *testing.T) {
	initial := testutil.Snapshot(t)
	backend := &testutil.Backend{State: testutil.Clone(initial), OnExecute: applyCommands}
	backend.State.Spaces[0].Index = 1
	backend.State.Spaces[1].Index = 0
	for i := range backend.State.Windows {
		w := &backend.State.Windows[i]
		index := *w.Space
		if index == 0 {
			index = 1
		} else if index == 1 {
			index = 0
		}
		w.Space = &index
	}
	result, err := actions.Run(context.Background(), backend, "bring", selected(t, initial, 101), initial, false)
	if err != nil || result.DestinationSpaceID != 10 || backend.Commands[0][4] != "1" {
		t.Fatalf("result=%v err=%v commands=%v", result, err, backend.Commands)
	}
}

func TestMoveFailureIsObservedAndNeverRetried(t *testing.T) {
	initial := testutil.Snapshot(t)
	backend := &testutil.Backend{State: testutil.Clone(initial), OnExecute: func(b *testutil.Backend, args []string) error {
		_ = applyCommands(b, args)
		return context.DeadlineExceeded
	}}
	result, err := actions.Run(context.Background(), backend, "bring", selected(t, initial, 101), initial, false)
	if err == nil || result.Status != "unconfirmed" || len(backend.Commands) != 1 || result.ObservedSpaceID == nil || *result.ObservedSpaceID != 10 {
		t.Fatalf("result=%v err=%v commands=%v", result, err, backend.Commands)
	}
}

func TestChangeAfterMoveReportsPartialAndSkipsFocus(t *testing.T) {
	initial := testutil.Snapshot(t)
	backend := &testutil.Backend{State: testutil.Clone(initial), OnExecute: func(b *testutil.Backend, args []string) error {
		_ = applyCommands(b, args)
		b.State.Windows[0].Title = "A different project now"
		return nil
	}}
	result, err := actions.Run(context.Background(), backend, "bring", selected(t, initial, 101), initial, false)
	if !errors.Is(err, actions.ErrStale) || result.Status != "partial" || len(backend.Commands) != 1 {
		t.Fatalf("result=%v err=%v commands=%v", result, err, backend.Commands)
	}
}

func TestFullscreenTopologyPreventsActivation(t *testing.T) {
	initial := testutil.Snapshot(t)
	initial.Spaces[2].Displays = []uint32{1}
	backend := &testutil.Backend{State: initial}
	_, err := actions.Run(context.Background(), backend, "focus", selected(t, initial, 101), initial, false)
	if err == nil || len(backend.Commands) != 0 {
		t.Fatal("activated unsupported fullscreen topology")
	}
}

func TestImmovableWindowPreventsBring(t *testing.T) {
	initial := testutil.Snapshot(t)
	movable := false
	initial.Windows[0].CanMove = &movable
	backend := &testutil.Backend{State: initial}
	_, err := actions.Run(context.Background(), backend, "bring", selected(t, initial, 101), initial, false)
	if err == nil || len(backend.Commands) != 0 {
		t.Fatal("submitted an unsupported window move")
	}
}
