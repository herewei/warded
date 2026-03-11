package application

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type DoctorService struct {
	ConfigStore     ports.LocalConfigStore
	ServeChecker    ports.ServeChecker
	ServeTLSChecker ports.ServeTLSChecker
}

type CheckResult struct {
	Name   string
	OK     bool
	Detail string
}

type DoctorOutput struct {
	Results []CheckResult
}

func (s DoctorService) Execute(ctx context.Context) (*DoctorOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("doctor service: config store is required")
	}

	results := make([]CheckResult, 0, 4)

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		results = append(results, CheckResult{Name: "ward_runtime", OK: false, Detail: "ward.json not found"})
	} else {
		jwtOK := runtime.JWTSigningSecret != ""
		jwtDetail := "jwt_signing_secret is present"
		if !jwtOK {
			jwtDetail = "jwt_signing_secret is missing"
		}
		results = append(results, CheckResult{Name: "local_jwt", OK: jwtOK, Detail: jwtDetail})

		results = append(results, CheckResult{
			Name:   "ward_runtime",
			OK:     true,
			Detail: fmt.Sprintf("ward_draft_id=%s ward_id=%s domain=%s", runtime.WardDraftID, runtime.WardID, runtime.Domain),
		})

		active := runtime.WardStatus == domain.WardStatusActive
		results = append(results, CheckResult{
			Name:   "ward_active",
			OK:     active,
			Detail: fmt.Sprintf("ward status is %s", runtime.WardStatus),
		})

		serveRunning := false
		if s.ServeChecker != nil {
			running, detail := s.ServeChecker.CheckServe(ctx)
			serveRunning = running
			results = append(results, CheckResult{
				Name:   "serve_running",
				OK:     running,
				Detail: detail,
			})
		}
		if s.ServeTLSChecker != nil {
			tlsResult := CheckResult{
				Name:   "tls_platform_cert",
				OK:     false,
				Detail: "skipped: serve is not running",
			}
			if serveRunning {
				addr := runtime.ListenAddr
				if addr == "" {
					addr = ":443"
				}
				fallback, detail := s.ServeTLSChecker.CheckServeTLS(ctx, addr, runtime.Domain)
				tlsResult.OK = !fallback
				tlsResult.Detail = detail
			}
			results = append(results, tlsResult)
		}

		integrationResult := CheckResult{
			Name:   "openclaw_integration",
			OK:     false,
			Detail: "skipped: ward is not active",
		}
		if active && runtime.Domain != "" {
			requiredOrigin, err := requiredOrigin("", runtime.Domain)
			if err != nil {
				integrationResult.Detail = fmt.Sprintf("failed to build required origin: %v", err)
			} else {
				configFile, err := openClawConfigPath("")
				if err != nil {
					integrationResult.Detail = fmt.Sprintf("failed to locate openclaw config: %v", err)
				} else {
					data, err := readFileFunc(configFile)
					switch {
					case errors.Is(err, os.ErrNotExist):
						integrationResult.Detail = fmt.Sprintf("config not found: %s", configFile)
					case err != nil:
						integrationResult.Detail = fmt.Sprintf("failed to read %s: %v", configFile, err)
					default:
						_, currentAllowed, desiredAllowed, err := updateOpenClawAllowedOrigins(data, requiredOrigin)
						if err != nil {
							integrationResult.Detail = fmt.Sprintf("invalid JSON in %s", configFile)
						} else if len(currentAllowed) == len(desiredAllowed) {
							integrationResult.OK = true
							integrationResult.Detail = fmt.Sprintf("allowedOrigins already includes %s", requiredOrigin)
						} else {
							integrationResult.Detail = fmt.Sprintf("allowedOrigins is missing %s", requiredOrigin)
						}
					}
				}
			}
		}
		results = append(results, integrationResult)
	}

	return &DoctorOutput{Results: results}, nil
}
