package cmd

import (
	"fmt"
	"io"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/spf13/cobra"
)

func newStatusCommand(version string) *cobra.Command {
	var dataDir string
	var baseDomain string
	var platformOrigin string
	var local bool

	command := &cobra.Command{
		Use:   "status",
		Short: "Show current ward and runtime status",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)
			service := application.StatusService{
				ConfigStore: store,
			}

			if !local {
				// Derive platform URL from the persisted site; skip if not yet initialised.
				runtime, err := store.LoadWardRuntime(cmd.Context())
				if err != nil {
					return fmt.Errorf("status: load runtime: %w", err)
				}
				if runtime != nil {
					url, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
					if err != nil {
						return fmt.Errorf("status: %w", err)
					}
					service.PlatformAPI = platformapi.NewClient(url, version)
				}
			}

			out, err := service.Execute(cmd.Context())
			if err != nil {
				return err
			}
			renderStatusOutput(cmd.OutOrStdout(), out)
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().BoolVar(&local, "local", false, "show local config only without calling the platform API")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example dev.warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")

	return command
}

func renderStatusOutput(w io.Writer, out *application.StatusOutput) {
	if out == nil || out.Runtime == nil {
		fmt.Fprintln(w, "ward: not attached")
	} else {
		fmt.Fprintf(w, "ward: draft_id=%s id=%s status=%s site=%s domain=%s upstream_port=%d billing_mode=%s activation_mode=%s activation_url=%s\n",
			out.Runtime.WardDraftID, out.Runtime.WardID, out.Runtime.WardStatus, out.Runtime.Site, out.Runtime.Domain, out.Runtime.UpstreamPort, out.Runtime.BillingMode, out.Runtime.ActivationMode, out.Runtime.ActivationURL)
	}

	if out != nil && out.WardDraft != nil {
		fmt.Fprintf(w, "draft: id=%s status=%s expires_at=%s\n",
			out.WardDraft.WardDraftID,
			out.WardDraft.Status,
			out.WardDraft.ExpiresAt,
		)
	}
}
