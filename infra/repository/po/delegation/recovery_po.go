package delegation

type Replay struct {
	ReplayID           string `gorm:"column:replay_id;primaryKey;size:64"`
	OwnerID            string `gorm:"column:owner_id;size:64;not null;index:idx_dso_replay_owner_status,priority:1"`
	SourceRunID        string `gorm:"column:source_run_id;size:64;not null;index"`
	SourceManifestID   string `gorm:"column:source_manifest_id;size:64;not null;index"`
	SourceManifestHash string `gorm:"column:source_manifest_hash;size:64;not null"`
	Mode               string `gorm:"column:mode;size:48;not null;index"`
	Status             string `gorm:"column:status;size:32;not null;index:idx_dso_replay_owner_status,priority:2"`
	RequestedBy        string `gorm:"column:requested_by;size:64;not null"`
	LiveApprovalRef    string `gorm:"column:live_approval_ref;size:128"`
	RequestContent     string `gorm:"column:request_content;type:text;not null"`
	ResultContent      string `gorm:"column:result_content;type:text"`
	ResultHash         string `gorm:"column:result_hash;size:64"`
	ErrorRef           string `gorm:"column:error_ref;type:text"`
	CreatedAt          int64  `gorm:"column:created_at;not null;index"`
	StartedAt          int64  `gorm:"column:started_at;not null"`
	EndedAt            int64  `gorm:"column:ended_at"`
}

func (Replay) TableName() string { return "os_dso_replay" }

type SchedulerLease struct {
	LeaseKey        string `gorm:"column:lease_key;primaryKey;size:128"`
	OwnerInstanceID string `gorm:"column:owner_instance_id;size:128;not null;index"`
	FencingToken    int64  `gorm:"column:fencing_token;not null"`
	Status          string `gorm:"column:status;size:24;not null;index"`
	AcquiredAt      int64  `gorm:"column:acquired_at;not null"`
	HeartbeatAt     int64  `gorm:"column:heartbeat_at;not null"`
	ExpiresAt       int64  `gorm:"column:expires_at;not null;index"`
	Revision        int64  `gorm:"column:revision;not null"`
}

func (SchedulerLease) TableName() string { return "os_dso_scheduler_lease" }

type RetentionTombstone struct {
	TombstoneID string `gorm:"column:tombstone_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	Cutoff      int64  `gorm:"column:cutoff;not null"`
	DeletedRows int64  `gorm:"column:deleted_rows;not null"`
	RequestedBy string `gorm:"column:requested_by;size:64;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
}

func (RetentionTombstone) TableName() string { return "os_dso_retention_tombstone" }
