// Package cli provides the command-line entry point and output contract.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/starkwm/swm-find/internal/actions"
	"github.com/starkwm/swm-find/internal/match"
	"github.com/starkwm/swm-find/internal/swm"
	"github.com/starkwm/swm-find/internal/typesafe"
)

const usage = `Usage: swm-find <match|focus|bring> [options] <window description>

  match   Report a matching window without changing the desktop.
  focus   Activate its normal desktop Space if needed, then focus it.
  bring   Move it to the active Space captured at invocation, then focus it.

Options go before the description. Run an operation with --help for options.
Jev receives your description and eligible window app names and titles.
Set TYPESAFE_API_KEY before using matching. No request or title logs are saved.
`

// Runner contains injectable dependencies for the CLI.
type Runner struct {
	Backend   swm.Backend
	Evaluator match.Evaluator
	Getenv    func(string) string
}

type output struct {
	match.Result
	Operation string          `json:"operation"`
	DryRun    bool            `json:"dry_run"`
	Action    *actions.Result `json:"action,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type stringsFlag []string

func (f *stringsFlag) String() string { return strings.Join(*f, ", ") }
func (f *stringsFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("excluded app name cannot be empty")
	}
	*f = append(*f, strings.TrimSpace(value))
	return nil
}

// Run parses arguments, resolves a window, and writes the result.
func (r Runner) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if args[0] == "--version" {
		fmt.Fprintln(stdout, "swm-find dev")
		return 0
	}
	operation := args[0]

	// Validate the requested operation before parsing any flags.
	if operation != "match" && operation != "focus" && operation != "bring" {
		fmt.Fprintln(stderr, "swm-find: expected match, focus, or bring")
		return 64
	}
	getenv := r.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "Write a JSON result, including non-match and action outcomes.")
	dryRun := flags.Bool("dry-run", false, "Show planned focus or bring commands without executing them.")
	swmPath := flags.String("swm", "swm", "Path to the swm executable.")
	modelDefault := getenv("SWM_FIND_MODEL")
	if modelDefault == "" {
		modelDefault = typesafe.DefaultModel
	}
	model := flags.String("model", modelDefault, "TypeSafe model ID.")
	apiTimeout := flags.Duration("api-timeout", 2*time.Second, "Deadline for the Jev request.")
	swmTimeout := flags.Duration("swm-timeout", 5*time.Second, "Deadline for each swm command.")
	policy := match.DefaultPolicy()
	flags.Float64Var(&policy.MinProbability, "min-probability", policy.MinProbability, "Minimum candidate probability.")
	flags.Float64Var(&policy.MinExists, "min-exists", policy.MinExists, "Minimum probability that a supported match exists.")
	flags.Float64Var(&policy.MinMargin, "min-margin", policy.MinMargin, "Minimum lead over the next candidate or none.")
	var excluded stringsFlag
	flags.Var(&excluded, "exclude-app", "Exclude an exact app name, case insensitive; repeat as needed.")
	flags.Usage = func() { fmt.Fprint(stderr, usage); flags.PrintDefaults() }
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 64
	}
	// Combine any remaining arguments into the natural-language window description.
	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" || len(query) > match.MaxDescriptionBytes || *apiTimeout <= 0 || *swmTimeout <= 0 || strings.TrimSpace(*model) == "" {
		fmt.Fprintln(stderr, "swm-find: provide a description of 1 to 4096 bytes, a model, and positive timeouts")
		return 64
	}
	if err := policy.Validate(); err != nil {
		fmt.Fprintln(stderr, "swm-find:", err)
		return 64
	}
	if operation == "match" && *dryRun {
		fmt.Fprintln(stderr, "swm-find: match already leaves the desktop unchanged")
		return 64
	}
	backend := r.Backend
	if backend == nil {
		backend = swm.Client{Path: *swmPath, Timeout: *swmTimeout}
	}
	evaluator := r.Evaluator
	if evaluator == nil {
		evaluator = typesafe.Client{APIKey: getenv("TYPESAFE_API_KEY"), Model: *model}
	}
	response := output{Operation: operation, DryRun: *dryRun, Result: match.Result{Status: "error", Candidates: []match.Candidate{}, Policy: policy}}
	snapshot, err := backend.Snapshot(ctx)
	if err == nil {
		requestCtx, cancel := context.WithTimeout(ctx, *apiTimeout)
		response.Result, err = match.Resolve(requestCtx, query, snapshot, excluded, evaluator, policy)
		cancel()
	}
	if err == nil && response.Status == "matched" && operation != "match" {
		var action actions.Result
		action, err = actions.Run(ctx, backend, operation, *response.Selected, snapshot, *dryRun)
		response.Action = &action
	}
	code := 0
	if err != nil {
		response.Error = err.Error()
		if response.Action == nil {
			response.Status = "error"
		}
		code = 1
	} else if response.Status != "matched" {
		code = 2
	}
	return writeResult(stdout, stderr, *jsonOutput, response, code)
}

func writeResult(stdout, stderr io.Writer, jsonOutput bool, response output, code int) int {
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(response); err != nil {
			fmt.Fprintln(stderr, "swm-find: could not write JSON result")
			return 1
		}
		return code
	}
	if err := printHuman(stdout, response); err != nil {
		fmt.Fprintln(stderr, "swm-find: could not write result")
		return 1
	}
	if response.Error != "" {
		fmt.Fprintln(stderr, "swm-find:", response.Error)
	}
	return code
}

func printHuman(out io.Writer, result output) error {
	if result.Status == "error" {
		return nil
	}
	if _, err := fmt.Fprintln(out, result.Status); err != nil {
		return err
	}
	limit := len(result.Candidates)
	if limit > 5 {
		limit = 5
	}
	for _, candidate := range result.Candidates[:limit] {
		// %q escapes titles containing newlines, terminal control characters, or instructions.
		if _, err := fmt.Fprintf(out, "  %d  %q  %q  %.3f\n", candidate.Window.ID, candidate.Window.App, candidate.Window.Title, candidate.Probability); err != nil {
			return err
		}
	}
	if result.Action != nil {
		if _, err := fmt.Fprintln(out, result.Action.Status); err != nil {
			return err
		}
		for _, step := range result.Action.Steps {
			if _, err := fmt.Fprintf(out, "  swm %s [%s]\n", strings.Join(step.Arguments, " "), step.Status); err != nil {
				return err
			}
		}
	}
	return nil
}
