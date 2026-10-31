// Package swm reads window state and executes explicit commands through swm.
package swm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"
)

// Window is one entry from swm's query windows output.
type Window struct {
	ID                 uint32  `json:"id"`
	PID                *int    `json:"pid"`
	App                string  `json:"app"`
	Title              string  `json:"title"`
	Role               string  `json:"role"`
	Subrole            string  `json:"subrole"`
	Space              *int    `json:"space"`
	Display            *uint32 `json:"display"`
	Layer              *int    `json:"layer"`
	CanMove            *bool   `json:"can-move"`
	HasAXReference     bool    `json:"has-ax-reference"`
	IsMinimized        bool    `json:"is-minimized"`
	IsNativeFullscreen bool    `json:"is-native-fullscreen"`
}

// Space is one entry from swm's query spaces output.
type Space struct {
	ID                 uint64   `json:"id"`
	Index              int      `json:"index"`
	Type               string   `json:"type"`
	Displays           []uint32 `json:"displays"`
	Windows            []uint32 `json:"windows"`
	HasFocus           bool     `json:"has-focus"`
	IsVisible          bool     `json:"is-visible"`
	IsNativeFullscreen bool     `json:"is-native-fullscreen"`
}

// Display is one entry from swm's query displays output.
type Display struct {
	ID     *uint32 `json:"id"`
	UUID   string  `json:"uuid"`
	Index  int     `json:"index"`
	Spaces []int   `json:"spaces"`
}

// Snapshot holds the windows, Spaces, and displays read at one point in time.
type Snapshot struct {
	Windows  []Window  `json:"windows"`
	Spaces   []Space   `json:"spaces"`
	Displays []Display `json:"displays"`
}

// Validate checks basic window and Space identity uniqueness.
func (s Snapshot) Validate() error {
	windows := make(map[uint32]bool)
	for _, w := range s.Windows {
		if w.ID == 0 || windows[w.ID] {
			return errors.New("window query has a missing or duplicate ID")
		}

		windows[w.ID] = true
	}

	spaces := make(map[uint64]bool)
	indexes := make(map[int]bool)
	for _, space := range s.Spaces {
		if space.ID == 0 || space.Index < 0 || spaces[space.ID] || indexes[space.Index] {
			return errors.New("Space query has a missing or duplicate ID or index")
		}

		spaces[space.ID], indexes[space.Index] = true, true
	}

	return nil
}

// ActiveSpace returns the single focused normal desktop Space.
func (s Snapshot) ActiveSpace() (Space, error) {
	var result Space
	count := 0
	for _, space := range s.Spaces {
		if space.HasFocus {
			result = space
			count++
		}
	}

	if count != 1 || !result.Normal() {
		return Space{}, errors.New("cannot identify one active normal desktop Space")
	}

	return result, nil
}

// Normal reports whether the Space is a non-fullscreen desktop Space.
func (s Space) Normal() bool { return s.Type == "normal" && !s.IsNativeFullscreen }

// SpaceByID looks up a Space by its stable ID.
func (s Snapshot) SpaceByID(id uint64) (Space, bool) {
	for _, space := range s.Spaces {
		if space.ID == id {
			return space, true
		}
	}

	return Space{}, false
}

// WindowSpace uses all Space memberships; window.space alone hides sticky windows.
func (s Snapshot) WindowSpace(w Window) (Space, error) {
	var result Space
	count := 0
	for _, space := range s.Spaces {
		for _, id := range space.Windows {
			if id == w.ID {
				result = space
				count++
				break
			}
		}
	}

	if count != 1 || !result.Normal() || w.Space == nil || *w.Space != result.Index {
		return Space{}, errors.New("window must belong to exactly one normal desktop Space")
	}

	return result, nil
}

// WindowByID looks up a Window by its ID.
func (s Snapshot) WindowByID(id uint32) (Window, bool) {
	for _, w := range s.Windows {
		if w.ID == id {
			return w, true
		}
	}

	return Window{}, false
}

// CanActivate mirrors swm's restriction on displays containing native fullscreen Spaces.
func (s Snapshot) CanActivate(target Space) error {
	if len(target.Displays) == 0 {
		return errors.New("destination Space has no known display")
	}

	for _, space := range s.Spaces {
		for _, display := range space.Displays {
			for _, targetDisplay := range target.Displays {
				if display == targetDisplay && !space.Normal() {
					return errors.New("Space activation is unavailable on a display containing fullscreen or unknown Spaces")
				}
			}
		}
	}

	return nil
}

// Backend abstracts swm process access.
type Backend interface {
	Snapshot(context.Context) (Snapshot, error)
	Execute(context.Context, ...string) error
}

// Client runs swm through bounded subprocesses.
type Client struct {
	Path    string
	Timeout time.Duration
}

// Each command gets its own deadline, including the daemon's asynchronous move.
func (c Client) run(ctx context.Context, args ...string) ([]byte, error) {
	path := c.Path
	if path == "" {
		path = "swm"
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	stdout := &boundedBuffer{limit: 4 << 20}
	cmd.Stdout = stdout
	// stderr can contain window metadata. Keep it out of default diagnostics.
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("swm command: %w", ctx.Err())
		}

		return nil, fmt.Errorf("swm command failed: %w", err)
	}

	return stdout.Bytes(), nil
}

// Snapshot queries windows, Spaces, and displays from swm.
func (c Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	queries := []struct {
		name  string
		value any
	}{
		{"windows", &snapshot.Windows}, {"spaces", &snapshot.Spaces}, {"displays", &snapshot.Displays},
	}

	for _, query := range queries {
		data, err := c.run(ctx, "query", query.name)
		if err != nil {
			return Snapshot{}, fmt.Errorf("query %s: %w", query.name, err)
		}

		if err := json.Unmarshal(data, query.value); err != nil {
			return Snapshot{}, fmt.Errorf("query %s returned invalid JSON", query.name)
		}
	}

	return snapshot, snapshot.Validate()
}

// Execute runs a swm command.
func (c Client) Execute(ctx context.Context, args ...string) error {
	_, err := c.run(ctx, args...)
	return err
}

// WindowID formats a window ID for swm command arguments.
func WindowID(id uint32) string { return strconv.FormatUint(uint64(id), 10) }

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, errors.New("swm output exceeds size limit")
	}

	return b.Buffer.Write(data)
}
