package swm_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/starkwm/swm-find/internal/swm"
	"github.com/starkwm/swm-find/internal/testutil"
)

func TestProcessAdapterReadsRealQuerySchema(t *testing.T) {
	dir := t.TempDir()
	snapshot := testutil.Snapshot(t)

	for name, value := range map[string]any{"windows": snapshot.Windows, "spaces": snapshot.Spaces, "displays": snapshot.Displays} {
		data, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(dir, "fake-swm")
	script := "#!/bin/sh\nif [ \"$1\" = query ]; then /bin/cat \"$SWM_FIND_TEST_DIR/$2.json\"; else printf '%s\\n' \"$@\" > \"$SWM_FIND_TEST_DIR/arguments\"; fi\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SWM_FIND_TEST_DIR", dir)
	client := swm.Client{Path: path}

	got, err := client.Snapshot(context.Background())
	if err != nil || len(got.Windows) != len(snapshot.Windows) {
		t.Fatalf("snapshot=%v err=%v", got, err)
	}

	if got.Windows[0].PID == nil || !got.Windows[0].HasAXReference || got.Windows[0].Space == nil {
		t.Fatal("lost nullable or hyphenated JSON fields")
	}

	if err := client.Execute(context.Background(), "window", "focus", "--window", "101"); err != nil {
		t.Fatal(err)
	}

	arguments, err := os.ReadFile(filepath.Join(dir, "arguments"))
	if err != nil || string(arguments) != "window\nfocus\n--window\n101\n" {
		t.Fatalf("args=%q err=%v", arguments, err)
	}
}

func TestProcessTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-swm")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec /bin/sleep 1\n"), 0700); err != nil {
		t.Fatal(err)
	}

	_, err := (swm.Client{Path: path, Timeout: 20 * time.Millisecond}).Snapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestSnapshotRejectsDuplicateIDsAndIndexes(t *testing.T) {
	for _, kind := range []string{"window ID", "Space ID", "Space index"} {
		t.Run(kind, func(t *testing.T) {
			snapshot := testutil.Snapshot(t)
			switch kind {
			case "window ID":
				snapshot.Windows[1].ID = snapshot.Windows[0].ID
			case "Space ID":
				snapshot.Spaces[1].ID = snapshot.Spaces[0].ID
			case "Space index":
				snapshot.Spaces[1].Index = snapshot.Spaces[0].Index
			}

			if snapshot.Validate() == nil {
				t.Fatal("accepted conflicting identity")
			}
		})
	}
}
