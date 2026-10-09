package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/testutil"
	"github.com/starkwm/swm-find/internal/typesafe"
)

func TestRequestPrivacyAndResponseContract(t *testing.T) {
	candidates, err := match.Candidates(testutil.Snapshot(t), []string{"Safari"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fake-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid API request")
		}
		body, _ := io.ReadAll(r.Body)
		for _, field := range []string{`"pid"`, `"space_id"`, `"display"`, `"id"`, "Safari", "starkwm/swm"} {
			if strings.Contains(string(body), field) {
				t.Errorf("sent excluded/local field %s", field)
			}
		}
		var request map[string]json.RawMessage
		if json.Unmarshal(body, &request) != nil || len(request) != 3 {
			t.Error("invalid request envelope")
		}
		var model string
		_ = json.Unmarshal(request["model"], &model)
		if model != typesafe.DefaultModel {
			t.Error("default model was not pinned")
		}
		p := testutil.Prediction(candidates, 101)
		_ = json.NewEncoder(w).Encode(map[string]any{"model": p.Model, "answers": map[string]any{
			"window": map[string]any{"type": "choice", "choice": p.Choice, "probabilities": p.Probabilities, "confidence": p.Confidence},
			"exists": map[string]any{"type": "noul", "noul": p.Exists},
		}})
	}))
	defer server.Close()
	client := typesafe.Client{APIKey: "fake-key", Endpoint: server.URL, HTTP: server.Client()}
	p, err := client.Evaluate(context.Background(), "the Memoir terminal", candidates)
	if err != nil || p.Choice != "w1" || p.Exists != 1 {
		t.Fatalf("prediction=%v error=%v", p, err)
	}
}

func TestUpstreamErrorsDoNotLeakBody(t *testing.T) {
	candidates, _ := match.Candidates(testutil.Snapshot(t), nil)
	for _, status := range []int{401, 422, 429, 529} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "PRIVATE_TITLE fake-key")
			}))
			defer server.Close()
			_, err := (typesafe.Client{APIKey: "fake-key", Endpoint: server.URL}).Evaluate(context.Background(), "query", candidates)
			if err == nil || strings.Contains(err.Error(), "PRIVATE_TITLE") || strings.Contains(err.Error(), "fake-key") {
				t.Fatalf("bad error: %v", err)
			}
		})
	}
}

func TestMalformedAnswersAreRejected(t *testing.T) {
	candidates, _ := match.Candidates(testutil.Snapshot(t), nil)
	for _, body := range []string{`{}`, `null`, `{"model":"jev","answers":{"window":{"type":"choice","choice":"w1"},"exists":{"type":"noul","noul":1}}}`, `{"model":"jev","answers":{"window":{"type":"score","confidence":1},"exists":{"type":"noul","noul":1}}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
		_, err := (typesafe.Client{APIKey: "fake-key", Endpoint: server.URL}).Evaluate(context.Background(), "query", candidates)
		server.Close()
		if err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestDeadlineCancelsRequest(t *testing.T) {
	candidates, _ := match.Candidates(testutil.Snapshot(t), nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := (typesafe.Client{APIKey: "fake-key", Endpoint: server.URL}).Evaluate(ctx, "query", candidates)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("request did not respect deadline: %v", err)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	candidates, _ := match.Candidates(testutil.Snapshot(t), nil)
	var called atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := (typesafe.Client{APIKey: "fake-key", Endpoint: server.URL}).Evaluate(context.Background(), "query", candidates)
	if err == nil || called.Load() {
		t.Fatal("followed an API redirect")
	}
}
