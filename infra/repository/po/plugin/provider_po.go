package plugin

type Provider struct {
	ProviderKey        string `gorm:"column:provider_key;primaryKey;type:varchar(300)" json:"provider_key"`
	ProviderID         string `gorm:"column:provider_id;type:varchar(220);not null;uniqueIndex:uk_plugin_provider_version" json:"provider_id"`
	Version            string `gorm:"column:version;type:varchar(80);not null;uniqueIndex:uk_plugin_provider_version" json:"version"`
	Name               string `gorm:"column:name;type:varchar(200);not null" json:"name"`
	Description        string `gorm:"column:description;type:text;not null" json:"description"`
	Status             string `gorm:"column:status;type:varchar(32);not null;index" json:"status"`
	Visibility         string `gorm:"column:visibility;type:varchar(32);not null;default:'private';index" json:"visibility"`
	Manifest           string `gorm:"column:manifest;type:text;not null" json:"-"`
	ManifestSHA256     string `gorm:"column:manifest_sha256;type:char(64);not null" json:"manifest_sha256"`
	Signature          string `gorm:"column:signature;type:text;not null" json:"-"`
	SBOM               string `gorm:"column:sbom;type:text;not null" json:"-"`
	GrantedPermissions string `gorm:"column:granted_permissions;type:text;not null" json:"-"`
	GrantedResources   string `gorm:"column:granted_resources;type:text;not null" json:"-"`
	ScanStatus         string `gorm:"column:scan_status;type:varchar(32);not null;default:'PENDING'" json:"scan_status"`
	ReviewStatus       string `gorm:"column:review_status;type:varchar(32);not null;default:'PENDING'" json:"review_status"`
	ReviewNotes        string `gorm:"column:review_notes;type:text" json:"review_notes,omitempty"`
	ApprovedBy         string `gorm:"column:approved_by;type:varchar(128)" json:"approved_by,omitempty"`
	RevokedReason      string `gorm:"column:revoked_reason;type:text" json:"revoked_reason,omitempty"`
	Revision           int64  `gorm:"column:revision;type:bigint;not null;default:1" json:"revision"`
	InstalledAt        int64  `gorm:"column:installed_at;type:bigint;not null" json:"installed_at"`
	UpdatedAt          int64  `gorm:"column:updated_at;type:bigint;not null" json:"updated_at"`
	DeletedAt          int64  `gorm:"column:deleted_at;type:bigint;not null;default:0;index" json:"-"`
}

func (*Provider) TableName() string { return "os_plugin_provider" }
