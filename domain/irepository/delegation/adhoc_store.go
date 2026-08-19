package delegation

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

type AdHocStore interface {
	CreateAdHocAdmission(context.Context, entity.AdHocAdmissionBundle) error
	FindAdHocOverlay(context.Context, string, string) (*entity.AdHocOverlay, *entity.OverlayAdmission, error)
	RecordAdHocOutcome(context.Context, entity.AdHocRunOutcome) error
	ListSuccessfulAdHocOutcomes(context.Context, string, string) ([]entity.AdHocRunOutcome, error)
	CreateProfileCandidate(context.Context, entity.ProfileCandidate, entity.Event) error
	FindProfileCandidate(context.Context, string, string) (*entity.ProfileCandidate, error)
	ListPendingProfileCandidates(context.Context, int) ([]entity.ProfileCandidate, error)
}
