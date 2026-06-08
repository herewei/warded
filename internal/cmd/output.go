package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

var (
	ErrInvalidFormat   = errors.New("invalid output format")
	ErrInvalidArgument = errors.New("invalid argument")
)

type Envelope struct {
	OK        bool         `json:"ok"`
	Command   string       `json:"command"`
	Mode      string       `json:"mode,omitempty"`
	Code      string       `json:"code,omitempty"`
	Event     string       `json:"event,omitempty"`
	Data      any          `json:"data,omitempty"`
	Error     *ErrorDetail `json:"error,omitempty"`
	Warnings  []Warning    `json:"warnings,omitempty"`
	NextSteps []NextStep   `json:"next_steps,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

type ErrorDetail struct {
	Code              string   `json:"code"`
	Reason            string   `json:"reason,omitempty"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	RequestID         string   `json:"request_id,omitempty"`
	RetryAfterSeconds *int     `json:"retry_after_seconds"`
	Retryable         bool     `json:"retryable"`
	Message           string   `json:"message,omitempty"`
	ResolvedPublicIP  string   `json:"resolved_public_ip,omitempty"`
	ResolvedDomainIPs []string `json:"resolved_domain_ips,omitempty"`
	ProbeURL          string   `json:"probe_url,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

type NextStep struct {
	Kind    string   `json:"kind"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

func outputFormat(cmd *cobra.Command) string {
	if cmd == nil {
		return "text"
	}
	value, err := cmd.Root().PersistentFlags().GetString("format")
	if err != nil || value == "" {
		return "text"
	}
	return value
}

func wantsJSON(cmd *cobra.Command) bool {
	return outputFormat(cmd) == "json"
}

func writeJSON(w io.Writer, env Envelope) {
	_ = json.NewEncoder(w).Encode(env)
}

func writeJSONError(cmd *cobra.Command, command, mode string, err error) {
	writeJSON(cmd.OutOrStdout(), Envelope{
		OK:        false,
		Command:   command,
		Mode:      mode,
		Error:     classifyError(err),
		RequestID: errorRequestID(err),
	})
	suppressCobraError(cmd)
}

func errorRequestID(err error) string {
	var platformErr *ports.PlatformError
	if errors.As(err, &platformErr) {
		return platformErr.RequestID
	}
	return ""
}

func writeJSONCommandError(cmd *cobra.Command, err error) {
	writeJSONError(cmd, commandNameForEnvelope(cmd), "", err)
}

func suppressCobraError(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	cmd.SilenceUsage = true
	if root := cmd.Root(); root != nil {
		root.SilenceErrors = true
		root.SilenceUsage = true
	}
}

func commandNameForEnvelope(cmd *cobra.Command) string {
	if cmd == nil {
		return "warded"
	}
	if cmd.HasParent() {
		return cmd.Name()
	}
	args := cmd.Flags().Args()
	if len(args) > 0 {
		return args[0]
	}
	return cmd.Name()
}

func jsonArgs(args cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, raw []string) error {
		err := args(cmd, raw)
		if err != nil && wantsJSON(cmd) {
			writeJSONCommandError(cmd, err)
		}
		return err
	}
}

func classifyError(err error) *ErrorDetail {
	if err == nil {
		return &ErrorDetail{Code: "internal_error", RetryAfterSeconds: nil}
	}
	var platformErr *ports.PlatformError
	if errors.As(err, &platformErr) {
		code := platformErr.Code
		if !knownPlatformErrorCode(code) {
			if platformErr.HTTPStatus >= 500 {
				code = "internal_error"
			} else {
				code = "platform_response_invalid"
			}
		}
		detail := &ErrorDetail{
			Code:              fallbackCode(code),
			Reason:            platformErr.Reason,
			HTTPStatus:        platformErr.HTTPStatus,
			RequestID:         platformErr.RequestID,
			RetryAfterSeconds: nil,
			Retryable:         retryableCode(code),
			ResolvedPublicIP:  platformErr.ResolvedPublicIP,
			ResolvedDomainIPs: platformErr.ResolvedDomainIPs,
			ProbeURL:          platformErr.ProbeURL,
		}
		if platformErr.RetryAfter > 0 {
			retryAfter := platformErr.RetryAfter
			detail.RetryAfterSeconds = &retryAfter
		}
		if detail.Code != "internal_error" {
			if platformErr.Message != "" {
				detail.Message = platformErr.Message
			} else {
				detail.Message = platformErr.Error()
			}
		}
		return detail
	}

	switch {
	case errors.Is(err, ErrInvalidFormat):
		return errorDetailWithMessage("invalid_format", err)
	case errors.Is(err, application.ErrDataDirNotWritable):
		return errorDetailWithMessage("data_dir_not_writable", err)
	case errors.Is(err, application.ErrListenPortPermission):
		return errorDetailWithMessage("listen_port_permission_denied", err)
	case errors.Is(err, application.ErrListenPortOccupied):
		return errorDetailWithMessage("listen_port_occupied", err)
	case errors.Is(err, application.ErrUpstreamUnreachable):
		return errorDetailWithMessage("upstream_unreachable", err)
	case errors.Is(err, application.ErrUnsupportedIngressDomainType):
		return errorDetailWithMessage("unsupported_ingress_domain_type", err)
	case errors.Is(err, ErrInvalidArgument):
		return errorDetailWithMessage("invalid_argument", err)
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no ward runtime") ||
		strings.Contains(msg, "no committed ward runtime") ||
		strings.Contains(msg, "ward.json not found"):
		return errorDetailWithMessage("config_not_found", err)
	case strings.Contains(msg, "required") ||
		strings.Contains(msg, "invalid ") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "accepts "):
		return errorDetailWithMessage("invalid_argument", err)
	}
	return &ErrorDetail{Code: "internal_error", RetryAfterSeconds: nil}
}

func errorDetailWithMessage(code string, err error) *ErrorDetail {
	detail := &ErrorDetail{Code: code, RetryAfterSeconds: nil}
	if err != nil {
		detail.Message = err.Error()
	}
	return detail
}

func fallbackCode(code string) string {
	if strings.TrimSpace(code) == "" {
		return "internal_error"
	}
	return code
}

func retryableCode(code string) bool {
	switch code {
	case "rate_limited", "ingress_unreachable", "platform_unreachable":
		return true
	default:
		return false
	}
}

func knownPlatformErrorCode(code string) bool {
	switch code {
	case "access_denied",
		"activation_link_expired",
		"auth_code_expired",
		"auth_code_invalid",
		"credential_expired",
		"forbidden",
		"domain_dns_not_ready",
		"domain_not_allowed",
		"domain_policy_violation",
		"domain_public_ip_mismatch",
		"domain_reserved",
		"domain_unavailable",
		"draft_secret_conflict",
		"ingress_unreachable",
		"internal_error",
		"not_found",
		"public_ip_unavailable",
		"rate_limited",
		"site_not_supported",
		"unsupported_ingress_domain_type",
		"trial_not_eligible",
		"ward_not_active":
		return true
	default:
		return false
	}
}

func formatError(value string) error {
	return fmt.Errorf("%w: unsupported value %q (text or json)", ErrInvalidFormat, value)
}

func invalidArgumentError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
}
