package delegation

type AdHocOverlay struct {
	OverlayID      string `gorm:"column:overlay_id;primaryKey;size:64"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index:idx_dso_adhoc_owner_status,priority:1"`
	BaseProfileRef string `gorm:"column:base_profile_ref;size:255;not null;index"`
	ContentHash    string `gorm:"column:content_hash;size:64;not null;index"`
	Status         string `gorm:"column:status;size:32;not null;index:idx_dso_adhoc_owner_status,priority:2"`
	Content        string `gorm:"column:content;type:text;not null"`
	ExpiresAt      int64  `gorm:"column:expires_at;not null;index"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
}

func (AdHocOverlay) TableName() string { return "os_dso_adhoc_overlay" }

type OverlayAdmission struct {
	DecisionID    string `gorm:"column:decision_id;primaryKey;size:64"`
	OverlayID     string `gorm:"column:overlay_id;size:64;not null;uniqueIndex"`
	OwnerID       string `gorm:"column:owner_id;size:64;not null;index"`
	Decision      string `gorm:"column:decision;size:32;not null;index"`
	PolicyVersion string `gorm:"column:policy_version;size:96;not null"`
	InputHash     string `gorm:"column:input_hash;size:64;not null;index"`
	Content       string `gorm:"column:content;type:text;not null"`
	CreatedAt     int64  `gorm:"column:created_at;not null;index"`
}

func (OverlayAdmission) TableName() string { return "os_dso_overlay_admission" }

type AdHocRunOutcome struct {
	OutcomeID    string `gorm:"column:outcome_id;primaryKey;size:64"`
	OverlayID    string `gorm:"column:overlay_id;size:64;not null;index:idx_dso_adhoc_outcome,priority:2"`
	OwnerID      string `gorm:"column:owner_id;size:64;not null;index:idx_dso_adhoc_outcome,priority:1;uniqueIndex:idx_dso_adhoc_owner_run,priority:1"`
	RunID        string `gorm:"column:run_id;size:64;not null;uniqueIndex:idx_dso_adhoc_owner_run,priority:2"`
	Status       string `gorm:"column:status;size:32;not null;index:idx_dso_adhoc_outcome,priority:3"`
	EvidenceRefs string `gorm:"column:evidence_refs;type:text"`
	CreatedAt    int64  `gorm:"column:created_at;not null;index"`
}

func (AdHocRunOutcome) TableName() string { return "os_dso_adhoc_run_outcome" }

type ProfileCandidate struct {
	CandidateID    string `gorm:"column:candidate_id;primaryKey;size:96"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index:idx_dso_profile_candidate_owner_status,priority:1;uniqueIndex:idx_dso_profile_candidate_overlay_owner,priority:1"`
	OverlayID      string `gorm:"column:overlay_id;size:64;not null;uniqueIndex:idx_dso_profile_candidate_overlay_owner,priority:2"`
	BaseProfileRef string `gorm:"column:base_profile_ref;size:255;not null"`
	ContentHash    string `gorm:"column:content_hash;size:64;not null;index"`
	Status         string `gorm:"column:status;size:32;not null;index:idx_dso_profile_candidate_owner_status,priority:2"`
	Content        string `gorm:"column:content;type:text;not null"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
}

func (ProfileCandidate) TableName() string { return "os_dso_profile_candidate" }
