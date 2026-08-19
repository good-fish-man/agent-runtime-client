package delegation

import "time"

const (
	AdHocOverlayAllowed = "ALLOWED"
	AdHocOverlayDenied  = "DENIED"
	AdHocOutcomeSuccess = "SUCCESS"
	AdHocOutcomeFailed  = "FAILED"
	ProfileReviewNeeded = "REVIEW_REQUIRED"
)

type AdHocOverlay struct {
	OverlayID      string
	OwnerID        string
	BaseProfileRef string
	ContentHash    string
	Status         string
	Content        string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

type OverlayAdmission struct {
	DecisionID    string
	OverlayID     string
	OwnerID       string
	Decision      string
	PolicyVersion string
	InputHash     string
	Content       string
	CreatedAt     time.Time
}

type AdHocAdmissionBundle struct {
	Overlay   AdHocOverlay
	Admission OverlayAdmission
	Event     Event
}

type AdHocRunOutcome struct {
	OutcomeID    string
	OverlayID    string
	OwnerID      string
	RunID        string
	Status       string
	EvidenceRefs string
	CreatedAt    time.Time
}

type ProfileCandidate struct {
	CandidateID    string
	OwnerID        string
	OverlayID      string
	BaseProfileRef string
	ContentHash    string
	Status         string
	Content        string
	CreatedAt      time.Time
}
