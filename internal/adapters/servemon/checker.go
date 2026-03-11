package servemon

import (
	"bytes"
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// SystemdChecker checks whether the local warded serve process is running.
// It tries `systemctl is-active <ServiceName>` first; if systemctl is unavailable
// (non-Linux or non-systemd host), it falls back to a TCP dial on FallbackPort.
type SystemdChecker struct {
	ServiceName  string // systemd unit name, defaults to "warded"
	FallbackPort int    // TCP port to probe when systemctl is unavailable, defaults to 443
}

func (c SystemdChecker) CheckServe(ctx context.Context) (bool, string) {
	name := c.ServiceName
	if name == "" {
		name = "warded"
	}

	if _, lookErr := exec.LookPath("systemctl"); lookErr == nil {
		var stdout bytes.Buffer
		cmd := exec.CommandContext(ctx, "systemctl", "is-active", name)
		cmd.Stdout = &stdout
		_ = cmd.Run()
		state := strings.TrimSpace(stdout.String())
		if state == "active" {
			return true, name + ".service is active"
		}
		if state != "" {
			return false, fmt.Sprintf("%s.service is %s", name, state)
		}
	}

	// Fallback: TCP port probe
	port := c.FallbackPort
	if port == 0 {
		port = 443
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err == nil {
		_ = conn.Close()
		return true, fmt.Sprintf("port %d is listening", port)
	}
	return false, fmt.Sprintf("warded.service not running (port %d unreachable)", port)
}

func (c SystemdChecker) CheckServeTLS(ctx context.Context, addr string, serverName string) (bool, string) {
	target := normalizeTLSProbeAddr(addr)
	if serverName == "" {
		serverName = "localhost"
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := cryptotls.DialWithDialer(dialer, "tcp", target, &cryptotls.Config{
		InsecureSkipVerify: true,
		ServerName:         serverName,
		MinVersion:         cryptotls.VersionTLS12,
	})
	if err != nil {
		return false, fmt.Sprintf("tls probe failed for %s: %v", target, err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return false, "tls probe returned no peer certificate"
	}
	cert := state.PeerCertificates[0]
	if isFallbackCertificate(cert) {
		return true, fmt.Sprintf("serving fallback self-signed certificate for %s", serverName)
	}
	return false, fmt.Sprintf("serving platform certificate issued by %s", cert.Issuer.CommonName)
}

func normalizeTLSProbeAddr(addr string) string {
	if strings.TrimSpace(addr) == "" {
		return "127.0.0.1:443"
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, port)
	}
	return addr
}

func isFallbackCertificate(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	return cert.Issuer.CommonName == "warded-fallback" && cert.Subject.CommonName == "warded-fallback"
}
