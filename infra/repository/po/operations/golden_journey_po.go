// Package operations contains append-only production verification records.
package operations

// GoldenJourneyResult is immutable evidence for one journey in a suite. The
// composite key permits all ten journeys to share one run ID without allowing a
// second write to replace previously reviewed evidence.
type GoldenJourneyResult struct {
	OwnerID       string `gorm:"column:owner_id;primaryKey;size:64"`
	RunID         string `gorm:"column:run_id;primaryKey;size:96"`
	JourneyID     string `gorm:"column:journey_id;primaryKey;size:64"`
	Verification  string `gorm:"column:verification_level;size:16;not null;index"`
	Status        string `gorm:"column:status;size:32;not null;index"`
	Content       string `gorm:"column:content;type:text;not null"`
	ContentSHA256 string `gorm:"column:content_sha256;size:64;not null"`
	TraceID       string `gorm:"column:trace_id;size:96;index"`
	Revision      int64  `gorm:"column:revision;not null"`
	CreatedAt     int64  `gorm:"column:created_at;not null;index:idx_ga_result_owner_created,priority:2"`
	UpdatedAt     int64  `gorm:"column:updated_at;not null"`
}

func (GoldenJourneyResult) TableName() string { return "os_ga_journey_result" }
