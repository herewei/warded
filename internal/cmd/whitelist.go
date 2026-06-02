package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/application/mapping"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

type whitelistMultipleRuntimesError struct {
	runtimes []application.RuntimeSummary
}

func (e whitelistMultipleRuntimesError) Error() string {
	return "multiple committed wards found: use --ward-id or an index"
}

func newWhitelistCommand() *cobra.Command {
	whitelistCmd := &cobra.Command{
		Use:   "whitelist",
		Short: "Manage auth bypass whitelist for a committed ward runtime",
	}

	whitelistCmd.AddCommand(
		newWhitelistAddCommand(),
		newWhitelistRemoveCommand(),
		newWhitelistListCommand(),
	)

	return whitelistCmd
}

func newWhitelistAddCommand() *cobra.Command {
	var dataDir string
	var wardID string
	var exact bool
	var prefix bool

	command := &cobra.Command{
		Use:   "add [index] [--exact|--prefix] <path>",
		Short: "Add a whitelist rule",
		Args:  jsonArgs(cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Helper to return errors with JSON envelope support
			returnErr := func(err error) error {
				if wantsJSON(cmd) {
					writeWhitelistJSONError(cmd, "whitelist add", err)
				}
				return err
			}

			var path string
			var resolveArgs []string
			if len(args) == 1 {
				path = args[0]
			} else {
				resolveArgs = args[:1]
				path = args[1]
			}
			if err := validateWhitelistFlags(exact, prefix, path); err != nil {
				return returnErr(err)
			}

			ruleType := "exact"
			if prefix {
				ruleType = "prefix"
			}

			store := storage.NewJSONStore(dataDir)
			runtime, err := resolveWhitelistTarget(cmd, store, wardID, resolveArgs)
			if err != nil {
				return returnErr(err)
			}

			for _, r := range runtime.AuthWhitelist {
				if r.Type == ruleType && r.Path == path {
					return returnErr(fmt.Errorf("rule already exists: %s %s", ruleType, path))
				}
			}

			runtime.AuthWhitelist = append(runtime.AuthWhitelist, domain.AuthWhitelistRule{
				Type: ruleType,
				Path: path,
			})
			runtime.UpdatedAt = time.Now().UTC()

			if saveErr := store.SaveWardRuntime(cmd.Context(), mapping.RuntimeRecordFromDomain(runtime)); saveErr != nil {
				return returnErr(fmt.Errorf("whitelist add: save: %w", saveErr))
			}

			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), Envelope{
					OK:      true,
					Command: "whitelist add",
					Data: map[string]any{
						"type": ruleType,
						"path": path,
					},
				})
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added whitelist rule: %s %s\n", ruleType, path)
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&wardID, "ward-id", "", "select a specific ward by its ID")
	command.Flags().BoolVar(&exact, "exact", false, "exact path match")
	command.Flags().BoolVar(&prefix, "prefix", false, "prefix path match")

	return command
}

func newWhitelistRemoveCommand() *cobra.Command {
	var dataDir string
	var wardID string
	var exact bool
	var prefix bool

	command := &cobra.Command{
		Use:   "remove [index] [--exact|--prefix] <path>",
		Short: "Remove a whitelist rule",
		Args:  jsonArgs(cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Helper to return errors with JSON envelope support
			returnErr := func(err error) error {
				if wantsJSON(cmd) {
					writeWhitelistJSONError(cmd, "whitelist remove", err)
				}
				return err
			}

			var path string
			var resolveArgs []string
			if len(args) == 1 {
				path = args[0]
			} else {
				resolveArgs = args[:1]
				path = args[1]
			}
			if err := validateWhitelistFlags(exact, prefix, path); err != nil {
				return returnErr(err)
			}

			ruleType := "exact"
			if prefix {
				ruleType = "prefix"
			}

			store := storage.NewJSONStore(dataDir)
			runtime, err := resolveWhitelistTarget(cmd, store, wardID, resolveArgs)
			if err != nil {
				return returnErr(err)
			}

			found := false
			newRules := runtime.AuthWhitelist[:0]
			for _, r := range runtime.AuthWhitelist {
				if r.Type == ruleType && r.Path == path {
					found = true
					continue
				}
				newRules = append(newRules, r)
			}
			if !found {
				return returnErr(fmt.Errorf("rule not found: %s %s", ruleType, path))
			}

			runtime.AuthWhitelist = newRules
			runtime.UpdatedAt = time.Now().UTC()

			if saveErr := store.SaveWardRuntime(cmd.Context(), mapping.RuntimeRecordFromDomain(runtime)); saveErr != nil {
				return returnErr(fmt.Errorf("whitelist remove: save: %w", saveErr))
			}

			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), Envelope{
					OK:      true,
					Command: "whitelist remove",
					Data: map[string]any{
						"type": ruleType,
						"path": path,
					},
				})
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed whitelist rule: %s %s\n", ruleType, path)
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&wardID, "ward-id", "", "select a specific ward by its ID")
	command.Flags().BoolVar(&exact, "exact", false, "exact path match")
	command.Flags().BoolVar(&prefix, "prefix", false, "prefix path match")

	return command
}

func newWhitelistListCommand() *cobra.Command {
	var dataDir string
	var wardID string

	command := &cobra.Command{
		Use:   "list [index]",
		Short: "List whitelist rules",
		Args:  jsonArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Helper to return errors with JSON envelope support
			returnErr := func(err error) error {
				if wantsJSON(cmd) {
					writeWhitelistJSONError(cmd, "whitelist list", err)
				}
				return err
			}

			store := storage.NewJSONStore(dataDir)
			runtime, err := resolveWhitelistTarget(cmd, store, wardID, args)
			if err != nil {
				return returnErr(err)
			}

			if wantsJSON(cmd) {
				rules := make([]map[string]string, 0, len(runtime.AuthWhitelist))
				for _, r := range runtime.AuthWhitelist {
					rules = append(rules, map[string]string{
						"type": r.Type,
						"path": r.Path,
					})
				}
				writeJSON(cmd.OutOrStdout(), Envelope{
					OK:      true,
					Command: "whitelist list",
					Data: map[string]any{
						"ward_id": runtime.WardID,
						"rules":   rules,
						"count":   len(runtime.AuthWhitelist),
					},
				})
				return nil
			}

			if len(runtime.AuthWhitelist) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No whitelist rules configured.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Whitelist rules for %s (%s):\n", runtime.WardID, runtime.Domain)
			for _, r := range runtime.AuthWhitelist {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n", r.Type, r.Path)
			}
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&wardID, "ward-id", "", "select a specific ward by its ID")

	return command
}

func validateWhitelistFlags(exact, prefix bool, path string) error {
	if exact && prefix {
		return fmt.Errorf("--exact and --prefix are mutually exclusive")
	}
	if !exact && !prefix {
		return fmt.Errorf("either --exact or --prefix is required")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with '/'")
	}
	if prefix && !strings.HasSuffix(path, "/") {
		fmt.Fprintf(os.Stderr, "Warning: --prefix path %q does not end with '/'; this may match unintended paths\n", path)
	}
	return nil
}

func writeWhitelistJSONError(cmd *cobra.Command, command string, err error) {
	var multiErr whitelistMultipleRuntimesError
	if errors.As(err, &multiErr) {
		writeJSON(cmd.OutOrStdout(), Envelope{
			OK:      false,
			Command: command,
			Error: &ErrorDetail{
				Code:              "invalid_argument",
				RetryAfterSeconds: nil,
			},
			Data: map[string]any{"runtimes": runtimeListDTO(multiErr.runtimes)},
		})
		suppressCobraError(cmd)
		return
	}
	writeJSONError(cmd, command, "", err)
}

func resolveWhitelistTarget(cmd *cobra.Command, store ports.LocalConfigStore, wardID string, args []string) (*domain.LocalWardRuntime, error) {
	listOut, err := application.StatusService{ConfigStore: store}.ListRuntimes(cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("whitelist: list runtimes: %w", err)
	}

	var committed []application.RuntimeSummary
	for _, rt := range listOut.Runtimes {
		if rt.Kind != application.RuntimeKindPendingConfig {
			committed = append(committed, rt)
		}
	}

	hasSelector := len(args) > 0 || wardID != ""

	if !hasSelector {
		switch len(committed) {
		case 0:
			return nil, fmt.Errorf("no committed ward runtime found")
		case 1:
			rt := committed[0]
			record, err := store.LoadRuntimeByID(cmd.Context(), runtimeListID(rt))
			if err != nil {
				return nil, fmt.Errorf("whitelist: load runtime: %w", err)
			}
			return mapping.DomainFromRuntimeRecord(record), nil
		default:
			if !wantsJSON(cmd) {
				fmt.Fprintln(cmd.OutOrStdout(), "Multiple committed wards found. Use --ward-id or an index to select one.")
				renderRuntimeList(cmd.OutOrStdout(), committed, "")
			}
			return nil, whitelistMultipleRuntimesError{runtimes: committed}
		}
	}

	matched, resolveErr := resolveStatusTarget(committed, args, wardID, "", "")
	if resolveErr != nil {
		return nil, resolveErr
	}

	id := runtimeListID(*matched)
	record, err := store.LoadRuntimeByID(cmd.Context(), id)
	if err != nil {
		return nil, fmt.Errorf("whitelist: load runtime: %w", err)
	}
	return mapping.DomainFromRuntimeRecord(record), nil
}
