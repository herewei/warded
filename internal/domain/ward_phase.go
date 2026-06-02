package domain

// LifecyclePhase is a derived, read-only view of the runtime lifecycle state.
type LifecyclePhase string

const (
	PhaseUninitialized LifecyclePhase = "uninitialized" // no draft, no ward
	PhaseDraftPending  LifecyclePhase = "draft_pending" // has draft, no ward
	PhaseActive        LifecyclePhase = "active"        // has ward and status=active
	PhaseExpired       LifecyclePhase = "expired"
	PhaseSuspended     LifecyclePhase = "suspended"
	PhaseDeleted       LifecyclePhase = "deleted"
)

// Phase returns the current lifecycle phase derived from the runtime fields.
//
// Invalid combinations (e.g. WardID != "" with an empty WardStatus) are
// conservatively treated as PhaseActive.
func (r *LocalWardRuntime) Phase() LifecyclePhase {
	if r == nil {
		return PhaseUninitialized
	}
	if r.WardDraftID == "" && r.WardID == "" {
		return PhaseUninitialized
	}
	if r.WardID == "" {
		return PhaseDraftPending
	}
	switch r.WardStatus {
	case WardStatusActive:
		return PhaseActive
	case WardStatusExpired:
		return PhaseExpired
	case WardStatusSuspended:
		return PhaseSuspended
	case WardStatusDeleted:
		return PhaseDeleted
	default:
		// WardID is present but WardStatus is empty or unrecognized;
		// treat as active to avoid blocking serve.
		return PhaseActive
	}
}
