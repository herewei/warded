package mapping

import (
	"fmt"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

const defaultHeartbeatInterval = 60 * time.Second

func heartbeatInterval(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultHeartbeatInterval
	}
	return time.Duration(seconds) * time.Second
}

// ApplyHeartbeatResponse updates the runtime fields from a heartbeat response
// and returns the next interval together with a terminal status error (if any).
// It does not call store.Save or touch agent token caches.
func ApplyHeartbeatResponse(
	runtime *domain.LocalWardRuntime,
	resp *ports.HeartbeatResponse,
	now time.Time,
) (nextInterval time.Duration, statusErr error) {
	if resp.WardStatus != "" {
		runtime.WardStatus = domain.WardStatus(resp.WardStatus)
	}
	if resp.ExpiresAt != "" {
		if expiresAt, parseErr := time.Parse(time.RFC3339, resp.ExpiresAt); parseErr == nil {
			runtime.ExpiresAt = expiresAt
		}
	}
	if resp.PlatformJWTPublicKeys != nil {
		runtime.PlatformJWTPublicKeys = resp.PlatformJWTPublicKeys
	}
	runtime.LastRefreshedAt = now
	runtime.UpdatedAt = now

	switch runtime.WardStatus {
	case "", domain.WardStatusActive:
		return heartbeatInterval(resp.NextHeartbeatAfter), nil
	case domain.WardStatusExpired:
		return defaultHeartbeatInterval, fmt.Errorf("ward has expired. Run 'warded new --commit' to create a new ward")
	case domain.WardStatusSuspended:
		return defaultHeartbeatInterval, fmt.Errorf("ward is suspended. Visit https://%s to resolve", runtime.Domain)
	case domain.WardStatusDeleted:
		return defaultHeartbeatInterval, fmt.Errorf("ward has been deleted. Run 'warded new --commit' to create a new ward")
	default:
		return defaultHeartbeatInterval, fmt.Errorf("ward status is %s, stopping serve", runtime.WardStatus)
	}
}
