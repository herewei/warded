package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/servemon"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/adapters/upstream"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

func newDoctorCommand(version string) *cobra.Command {
	var dataDir string
	var agent string
	var baseline bool
	var preflight bool
	var site string
	var listenHost string
	var listenV6Host string
	var listenPort int
	var upstreamAddr string
	var domainType string
	var requestedDomain string
	var baseDomain string
	var platformOrigin string
	var openClawPath string

	command := &cobra.Command{
		Use:   "doctor",
		Short: "Run interactive diagnostics for the current node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if preflight {
				listenHostInput, listenPortInput, portChanged := splitDoctorListenInput(listenHost, listenPort, cmd.Flags().Changed("listen"), cmd.Flags().Changed("port"))
				service := application.DoctorPreflightService{
					DataDirCheck:   doctorDataDirChecker{},
					ListenResolver: doctorListenResolver{},
					ListenCheck:    doctorListenChecker{},
					UpstreamCheck:  upstream.NewChecker(),
					DNSResolver:    doctorDNSResolver{},
					ChallengeGen:   doctorProbeChallengeGenerator{},
					ProbeServer:    doctorProbeServer{},
					IngressProbe:   doctorIngressProbeClientFactory{},
				}
				out, err := service.Execute(cmd.Context(), application.DoctorPreflightInput{
					DataDir:         dataDir,
					Site:            site,
					ListenHost:      listenHostInput,
					ListenV6Host:    listenV6Host,
					ListenPort:      listenPortInput,
					UpstreamAddr:    upstreamAddr,
					DomainType:      domainType,
					RequestedDomain: requestedDomain,
					BaseDomain:      baseDomain,
					PlatformOrigin:  platformOrigin,
					Version:         version,
					ListenChanged:   cmd.Flags().Changed("listen"),
					ListenV6Changed: cmd.Flags().Changed("listen-v6"),
					PortChanged:     portChanged,
				})
				if err != nil {
					if wantsJSON(cmd) {
						writeDoctorPreflightJSON(cmd, out, err)
					}
					return err
				}
				if wantsJSON(cmd) {
					writeDoctorPreflightJSON(cmd, out, nil)
				} else {
					renderDoctorPreflight(cmd.OutOrStdout(), out)
				}
				return nil
			}
		store := storage.NewJSONStore(dataDir)
		serveMon := servemon.ServeMonitor{}
		if !baseline {
			runtime, err := store.LoadWardRuntime(cmd.Context())
			if err != nil {
				return err
			}
			if runtime != nil {
				serveMon.FallbackPort = runtime.ListenPort
				serveMon.FallbackFamily = runtime.IngressFamily
			}
		}
		var cli application.OpenClawCLI
		if baseline || agent != "" {
			c, err := application.NewOpenClawCLI(openClawPath)
			if err != nil {
				if wantsJSON(cmd) {
					mode := ""
					if baseline {
						mode = "baseline"
					}
					writeJSONError(cmd, "doctor", mode, err)
					return nil
				}
				return err
			}
			cli = c
		}
		service := application.DoctorService{
			ConfigStore:     store,
			ServeMonitor:    serveMon,
			ServeTLSMonitor: serveMon,
			OpenClawCLI:     cli,
		}
		out, err := service.Execute(cmd.Context(), application.DoctorInput{
			Agent:        agent,
			Baseline:     baseline,
			OpenClawPath: openClawPath,
		})
			if err != nil {
				if wantsJSON(cmd) {
					mode := ""
					if baseline {
						mode = "baseline"
					}
					writeJSONError(cmd, "doctor", mode, err)
				}
				return err
			}

			if wantsJSON(cmd) {
				mode := ""
				if baseline {
					mode = "baseline"
				}
				writeJSON(cmd.OutOrStdout(), Envelope{
					OK:      true,
					Command: "doctor",
					Mode:    mode,
					Data:    doctorOutputDTO(out),
				})
				return nil
			}

			printWardHeader(cmd.OutOrStdout(), doctorWardLabel(out))

			if baseline {
				renderBaselineDoctor(cmd.OutOrStdout(), out)
			} else {
				renderLegacyDoctor(cmd.OutOrStdout(), out)
			}
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&agent, "agent", "", "target agent for diagnosis (e.g. openclaw)")
	command.Flags().BoolVar(&baseline, "baseline", false, "run OpenClaw security baseline diagnosis")
	command.Flags().BoolVar(&preflight, "preflight", false, "verify this host can run warded before creating a ward")
	command.Flags().StringVar(&site, "site", "", "target site: cn (warded.cn) or global (warded.me)")
	command.Flags().StringVar(&listenHost, "listen", "0.0.0.0", "IPv4 listen host for warded serve")
	command.Flags().StringVar(&listenV6Host, "listen-v6", "", "IPv6 listen host for warded serve")
	command.Flags().IntVar(&listenPort, "port", 443, "listen port for warded serve")
	command.Flags().StringVar(&upstreamAddr, "upstream", "", "upstream address to protect (host:port); default 127.0.0.1:18789")
	command.Flags().StringVar(&domainType, "domain-type", string(domain.DomainTypePlatformSubdomain), "domain type: platform_subdomain or custom_domain")
	command.Flags().StringVar(&requestedDomain, "domain", "", "requested full domain for custom_domain preflight")
	command.Flags().StringVar(&requestedDomain, "requested-domain", "", "requested full domain for custom_domain preflight")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin")
	command.Flags().StringVar(&openClawPath, "openclaw-path", "", "path to the openclaw binary (auto-detected from PATH if empty)")
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

func splitDoctorListenInput(listenHost string, listenPort int, listenChanged, portChanged bool) (string, int, bool) {
	if !listenChanged || strings.TrimSpace(listenHost) == "" || portChanged {
		return listenHost, listenPort, portChanged
	}
	host, port, err := net.SplitHostPort(listenHost)
	if err == nil {
		if parsed := extractPortFromAddr(net.JoinHostPort(host, port)); parsed > 0 {
			return host, parsed, true
		}
	}
	if strings.Count(listenHost, ":") == 1 {
		idx := strings.LastIndex(listenHost, ":")
		if idx > 0 {
			host = listenHost[:idx]
			if parsed := extractPortFromAddr(listenHost); parsed > 0 {
				return host, parsed, true
			}
		}
	}
	return listenHost, listenPort, portChanged
}

type doctorDataDirChecker struct{}

func (doctorDataDirChecker) EnsureWritable(path string) error {
	return ensureDataDirWritable(path)
}

type doctorListenResolver struct{}

func (doctorListenResolver) ResolveListen(existing *domain.LocalWardRuntime, listenHost, listenV6Host string, listenPort int, listenChanged, listenV6Changed, portChanged bool) (string, int, domain.IngressFamily, error) {
	return resolveListenParams(existing, listenHost, listenV6Host, listenPort, listenChanged, listenV6Changed, portChanged)
}

type doctorListenChecker struct{}

func (doctorListenChecker) EnsureAvailable(addr string) error {
	return ensureAddrAvailable(addr)
}

type doctorDNSResolver struct{}

func (doctorDNSResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

type doctorProbeChallengeGenerator struct{}

func (doctorProbeChallengeGenerator) GenerateProbeChallenge() (string, error) {
	return randomProbeChallenge()
}

type doctorProbeServer struct{}

func (doctorProbeServer) StartProbeServer(ctx context.Context, addr string) (func(context.Context) error, error) {
	return startTemporaryProbeServerAddr(ctx, addr)
}

type doctorIngressProbeClientFactory struct{}

func (doctorIngressProbeClientFactory) NewIngressProbeAPI(site domain.Site, baseDomain, platformOrigin, version string) (ports.IngressProbeAPI, error) {
	platformURL, err := resolvePlatformOrigin(site, baseDomain, platformOrigin)
	if err != nil {
		return nil, err
	}
	return platformapi.NewClient(platformURL, version), nil
}

func renderLegacyDoctor(w io.Writer, out *application.DoctorOutput) {
	for _, result := range out.Results {
		status := "FAIL"
		if result.State == application.CheckOK {
			status = "OK"
		}
		fmt.Fprintf(w, "  [%-4s]  %-20s  %s\n", status, result.Name, result.Detail)
	}
	fmt.Fprintln(w)
}

func renderBaselineDoctor(w io.Writer, out *application.DoctorOutput) {
	passed := 0
	total := 0

	for _, r := range out.Results {
		state := string(r.State)
		fmt.Fprintf(w, "  %d. [%-4s] %s\n", r.Number, state, r.Name)
		fmt.Fprintf(w, "     %s\n", r.Detail)
		if r.State == application.CheckOK {
			passed++
			total++
		} else if r.State == application.CheckFAIL {
			total++
		}
	}

	if total == 0 {
		fmt.Fprintf(w, "\n  Summary: no actionable checks\n")
	} else {
		fmt.Fprintf(w, "\n  Summary: %d/%d checks passed\n", passed, total)
	}
	if out.BaselineOK {
		fmt.Fprintf(w, "  Baseline: acceptable\n")
	} else {
		fmt.Fprintf(w, "  Baseline: needs repair\n")
	}

	if len(out.NextSteps) > 0 {
		fmt.Fprintf(w, "\n  Next steps:\n")
		for _, step := range out.NextSteps {
			fmt.Fprintf(w, "    - %s\n", step)
		}
	}
	fmt.Fprintln(w)
}

func renderDoctorPreflight(w io.Writer, out *application.DoctorPreflightOutput) {
	passed := 0
	total := 0
	for _, r := range out.Results {
		state := string(r.State)
		fmt.Fprintf(w, "  %d. [%-4s] %s\n", r.Number, state, r.Name)
		if r.Detail != "" {
			fmt.Fprintf(w, "     %s\n", r.Detail)
		}
		if r.State == application.CheckOK {
			passed++
			total++
		} else if r.State == application.CheckFAIL {
			total++
		}
	}
	fmt.Fprintf(w, "\n  Summary: %d/%d checks passed\n", passed, total)
	if doctorPreflightSucceeded(out) {
		fmt.Fprintf(w, "  Preflight: this host can run warded\n")
		fmt.Fprintf(w, "\n  Next steps:\n")
		fmt.Fprintf(w, "    - %s\n", doctorPreflightNewCommand(out))
		fmt.Fprintf(w, "    - review the setup, then run `warded new --commit`\n")
	} else {
		fmt.Fprintf(w, "  Preflight: this host cannot run warded yet\n")
		fmt.Fprintf(w, "\n  Next steps:\n")
		fmt.Fprintf(w, "    - fix the failed check above\n")
		fmt.Fprintf(w, "    - rerun `warded doctor --preflight` after fixing\n")
	}
	fmt.Fprintln(w)
}

func writeDoctorPreflightJSON(cmd *cobra.Command, out *application.DoctorPreflightOutput, err error) {
	if out == nil {
		out = &application.DoctorPreflightOutput{}
	}
	if err != nil {
		writeJSON(cmd.OutOrStdout(), Envelope{
			OK:        false,
			Command:   "doctor",
			Mode:      "preflight",
			Error:     classifyError(err),
			RequestID: errorRequestID(err),
		})
		suppressCobraError(cmd)
		return
	}
	writeJSON(cmd.OutOrStdout(), Envelope{
		OK:        true,
		Command:   "doctor",
		Mode:      "preflight",
		Data:      doctorPreflightDTO(out),
		NextSteps: doctorPreflightNextSteps(out),
	})
}

func doctorOutputDTO(out *application.DoctorOutput) map[string]any {
	data := map[string]any{
		"checks": doctorChecksDTO(out.Results),
	}
	if out.BaselineOK {
		data["baseline"] = "acceptable"
	} else if len(out.Results) > 0 {
		data["baseline"] = "needs_repair"
	}
	return data
}

func doctorPreflightDTO(out *application.DoctorPreflightOutput) map[string]any {
	data := map[string]any{
		"checks": doctorChecksDTO(out.Results),
	}
	if out.Site != "" {
		data["site"] = out.Site
	}
	if out.ListenPort > 0 {
		data["listen"] = formatListenPartsForJSON(out.ListenHost, out.ListenPort, out.IngressFamily)
	}
	if out.UpstreamAddr != "" {
		data["upstream"] = out.UpstreamAddr
	}
	if out.DomainType != "" {
		data["domain_type"] = out.DomainType
	}
	if out.RequestedDomain != "" {
		data["requested_domain"] = out.RequestedDomain
	}
	if out.ResolvedPublicIP != "" {
		data["resolved_public_ip"] = out.ResolvedPublicIP
	}
	return data
}

func doctorChecksDTO(results []application.CheckResult) []map[string]any {
	checks := make([]map[string]any, 0, len(results))
	for _, r := range results {
		check := map[string]any{
			"number": r.Number,
			"key":    r.Key,
			"status": strings.ToLower(string(r.State)),
		}
		if r.Detail != "" {
			check["detail"] = map[string]any{"message": r.Detail}
		}
		checks = append(checks, check)
	}
	return checks
}

func doctorPreflightNextSteps(out *application.DoctorPreflightOutput) []NextStep {
	args := doctorPreflightNewArgs(out)
	return []NextStep{{Kind: "command", Command: "warded", Args: args}}
}

func doctorPreflightSucceeded(out *application.DoctorPreflightOutput) bool {
	if out == nil || len(out.Results) == 0 {
		return false
	}
	for _, result := range out.Results {
		if result.State == application.CheckFAIL {
			return false
		}
	}
	return true
}

func doctorPreflightNewCommand(out *application.DoctorPreflightOutput) string {
	return "warded " + strings.Join(doctorPreflightNewArgs(out), " ")
}

func doctorPreflightNewArgs(out *application.DoctorPreflightOutput) []string {
	args := []string{"new", "--site", string(out.Site)}
	if out.ListenPort > 0 {
		args = append(args, "--listen", out.ListenHost, "--port", fmt.Sprintf("%d", out.ListenPort))
	}
	if out.UpstreamAddr != "" {
		args = append(args, "--upstream", out.UpstreamAddr)
	}
	if out.DomainType != "" && out.DomainType != domain.DomainTypePlatformSubdomain {
		args = append(args, "--domain-type", string(out.DomainType))
	}
	if out.RequestedDomain != "" {
		args = append(args, "--domain", out.RequestedDomain)
	}
	return args
}

func formatListenPartsForJSON(host string, port int, family domain.IngressFamily) string {
	if family == domain.IngressFamilyIPv6 {
		return fmt.Sprintf("ipv6 [%s]:%d", host, port)
	}
	return fmt.Sprintf("ipv4 %s:%d", host, port)
}

func doctorWardLabel(out *application.DoctorOutput) string {
	if out == nil || out.Runtime == nil {
		return "(not configured)"
	}
	runtime := out.Runtime
	if runtime.Domain != "" {
		// For active wards, add status suffix for non-active states
		status := string(runtime.WardStatus)
		switch status {
		case "active":
			return runtime.Domain
		case "expired":
			return fmt.Sprintf("%s (expired)", runtime.Domain)
		case "suspended":
			return fmt.Sprintf("%s (suspended)", runtime.Domain)
		case "deleted":
			return fmt.Sprintf("%s (deleted)", runtime.Domain)
		default:
			return runtime.Domain
		}
	}
	if runtime.RequestedDomain != "" && runtime.WardID == "" {
		status := string(runtime.WardStatus)
		switch status {
		case "", "initializing", "pending_activation", "activating":
			return fmt.Sprintf("%s (pending)", runtime.RequestedDomain)
		case "expired":
			return fmt.Sprintf("%s (expired)", runtime.RequestedDomain)
		case "failed":
			return fmt.Sprintf("%s (failed)", runtime.RequestedDomain)
		default:
			return runtime.RequestedDomain
		}
	}
	if runtime.WardStatus == domain.WardStatusInitializing || runtime.WardDraftID != "" {
		return "(pending setup)"
	}
	return "(not configured)"
}
