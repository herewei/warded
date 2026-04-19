package cmd

import (
	"fmt"
	"strings"

	"github.com/herewei/warded/internal/adapters/servemon"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	var dataDir string

	command := &cobra.Command{
		Use:   "doctor",
		Short: "Run interactive diagnostics for the current node",
		RunE: func(cmd *cobra.Command, args []string) error {
			serveMon := servemon.SystemdChecker{}
			service := application.DoctorService{
				ConfigStore:     storage.NewJSONStore(dataDir),
				ServeChecker:    serveMon,
				ServeTLSChecker: serveMon,
			}
			out, err := service.Execute(cmd.Context())
			if err != nil {
				return err
			}

			var domain string
			for _, result := range out.Results {
				if result.Name == "ward_runtime" && result.OK {
					domain = extractDomain(result.Detail)
					break
				}
			}

			fmt.Fprintln(cmd.OutOrStdout())
			if domain != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "╭──────────────────────────────────────────────╮\n")
				fmt.Fprintf(cmd.OutOrStdout(), "│  Ward: %-40s │\n", domain)
				fmt.Fprintf(cmd.OutOrStdout(), "╰──────────────────────────────────────────────╯\n")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "╭──────────────────────────────────────────────╮\n")
				fmt.Fprintf(cmd.OutOrStdout(), "│  Ward: (not configured)                      │\n")
				fmt.Fprintf(cmd.OutOrStdout(), "╰──────────────────────────────────────────────╯\n")
			}
			fmt.Fprintln(cmd.OutOrStdout())

			for _, result := range out.Results {
				status := "FAIL"
				if result.OK {
					status = "OK"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%-4s]  %-20s  %s\n", status, result.Name, result.Detail)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")

	return command
}

func extractDomain(detail string) string {
	parts := strings.Split(detail, " ")
	for _, part := range parts {
		if strings.HasPrefix(part, "domain=") {
			return strings.TrimPrefix(part, "domain=")
		}
	}
	return ""
}
