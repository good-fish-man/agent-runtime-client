package knowledge

type Claim struct {
	ClaimID     string `gorm:"column:claim_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index:idx_knowledge_claim_lookup,priority:1"`
	Subject     string `gorm:"column:subject;size:256;not null;index:idx_knowledge_claim_lookup,priority:2"`
	Predicate   string `gorm:"column:predicate;size:128;not null;index:idx_knowledge_claim_lookup,priority:3"`
	Scope       string `gorm:"column:scope;size:24;not null;index"`
	Sensitivity string `gorm:"column:sensitivity;size:24;not null;index"`
	Status      string `gorm:"column:status;size:24;not null;index"`
	ValidUntil  int64  `gorm:"column:valid_until;index"`
	SearchText  string `gorm:"column:search_text;type:text;not null"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt   int64  `gorm:"column:updated_at;not null;index"`
}

func (Claim) TableName() string { return "os_knowledge_claim" }

type Evidence struct {
	EvidenceID  string  `gorm:"column:evidence_id;primaryKey;size:64"`
	OwnerID     string  `gorm:"column:owner_id;size:64;not null;index"`
	Scope       string  `gorm:"column:scope;size:24;not null;index"`
	Sensitivity string  `gorm:"column:sensitivity;size:24;not null;index"`
	SourceType  string  `gorm:"column:source_type;size:32;not null;index"`
	URI         string  `gorm:"column:uri;type:text"`
	Accessible  bool    `gorm:"column:accessible;not null;index"`
	Authority   float64 `gorm:"column:authority;not null"`
	Freshness   float64 `gorm:"column:freshness;not null"`
	Content     string  `gorm:"column:content;type:text;not null"`
	CreatedAt   int64   `gorm:"column:created_at;not null;index"`
}

func (Evidence) TableName() string { return "os_evidence" }

type Contradiction struct {
	ContradictionID string `gorm:"column:contradiction_id;primaryKey;size:64"`
	OwnerID         string `gorm:"column:owner_id;size:64;not null;index"`
	Resolved        bool   `gorm:"column:resolved;not null;index"`
	Content         string `gorm:"column:content;type:text;not null"`
	CreatedAt       int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt       int64  `gorm:"column:updated_at;not null"`
}

func (Contradiction) TableName() string { return "os_contradiction" }

type Snapshot struct {
	SnapshotID string `gorm:"column:snapshot_id;primaryKey;size:64"`
	OwnerID    string `gorm:"column:owner_id;size:64;not null;index"`
	Checksum   string `gorm:"column:checksum;size:64;not null;index"`
	Content    string `gorm:"column:content;type:text;not null"`
	CreatedAt  int64  `gorm:"column:created_at;not null;index"`
}

func (Snapshot) TableName() string { return "os_knowledge_snapshot" }

type OntologyPack struct {
	PackID    string `gorm:"column:pack_id;primaryKey;size:64"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index"`
	Name      string `gorm:"column:name;size:128;not null"`
	Domain    string `gorm:"column:domain;size:128;not null;index"`
	Current   string `gorm:"column:current_version;size:32"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
}

func (OntologyPack) TableName() string { return "os_ontology_pack" }

type OntologyVersion struct {
	VersionID string `gorm:"column:version_id;primaryKey;size:64"`
	PackID    string `gorm:"column:pack_id;size:64;not null;uniqueIndex:idx_ontology_pack_version,priority:1"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index"`
	Version   string `gorm:"column:version;size:32;not null;uniqueIndex:idx_ontology_pack_version,priority:2"`
	Status    string `gorm:"column:status;size:24;not null;index"`
	Checksum  string `gorm:"column:checksum;size:64;not null"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
}

func (OntologyVersion) TableName() string { return "os_ontology_version" }

type OntologyCandidate struct {
	CandidateID string `gorm:"column:candidate_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	PackID      string `gorm:"column:pack_id;size:64;not null;index"`
	Status      string `gorm:"column:status;size:32;not null;index"`
	Revision    int64  `gorm:"column:revision;not null"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null"`
	UpdatedAt   int64  `gorm:"column:updated_at;not null"`
}

func (OntologyCandidate) TableName() string { return "os_ontology_candidate" }

type OntologyMigration struct {
	MigrationID string `gorm:"column:migration_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	PackID      string `gorm:"column:pack_id;size:64;not null;index"`
	Status      string `gorm:"column:status;size:24;not null;index"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null"`
}

func (OntologyMigration) TableName() string { return "os_ontology_migration" }
