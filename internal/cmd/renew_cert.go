package cmd

import (
	"fmt"
	"time"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/spf13/cobra"
)

func newRenewCertCommand(version string) *cobra.Command {
	var (
		dataDir        string
		baseDomain     string
		platformOrigin string
	)

	command := &cobra.Command{
		Use:   "renew-cert",
		Short: "Check and refresh the TLS certificate from the platform",
		Long: `Fetch the current TLS certificate from the Warded platform and record
the renewal timestamp locally. Run this command periodically (e.g. every 7 days)
to keep the local certificate fresh.

Only applies to platform-managed wildcard certificates (starter spec).
Custom domain certificates are renewed automatically by the built-in ACME client.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)

			runtime, err := store.LoadWardRuntime(cmd.Context())
			if err != nil {
				return fmt.Errorf("renew-cert: %w", err)
			}
			if runtime == nil {
				return fmt.Errorf("renew-cert: no ward runtime found — run 'warded new --commit' first")
			}

			platformURL, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
			if err != nil {
				return fmt.Errorf("renew-cert: %w", err)
			}

			service := application.RenewCertService{
				ConfigStore: store,
				PlatformAPI: platformapi.NewClient(platformURL, version),
			}

			out, err := service.Execute(cmd.Context())
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "  Domain:       %s\n", out.Domain)
			if !out.NotAfter.IsZero() {
				expiry := out.NotAfter.Format("2006-01-02")
				if out.DaysRemaining <= 14 {
					fmt.Fprintf(cmd.OutOrStdout(), "  Valid until:  %s (%d days) ⚠ expiring soon\n", expiry, out.DaysRemaining)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  Valid until:  %s (%d days)\n", expiry, out.DaysRemaining)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  Last renewed: %s\n", out.LastRenewedAt.Format(time.RFC3339))
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Certificate refreshed.")
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin")

	// Hide development/testing flags from help output
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}
