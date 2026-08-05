package browser

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// SiteCredential stores ownership and display metadata only. The password is
// encrypted by agent-browser's Auth Vault and never enters PostgreSQL.
type SiteCredential struct {
	Ulid           string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
	UpdatedAt      int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint" json:"updated_at"`
	DeletedAt      int64  `gorm:"column:deleted_at;type:bigint;default:0;index" json:"-"`
	UserID         string `gorm:"column:user_id;type:varchar(128);not null;uniqueIndex:uk_site_credential_owner_vault;index:idx_site_credential_owner_domain" json:"-"`
	Name           string `gorm:"column:name;type:varchar(120);not null" json:"name"`
	Domain         string `gorm:"column:domain;type:varchar(255);not null;index:idx_site_credential_owner_domain" json:"domain"`
	LoginURL       string `gorm:"column:login_url;type:varchar(1000);not null" json:"login_url"`
	UsernameMasked string `gorm:"column:username_masked;type:varchar(255);not null" json:"username_masked"`
	VaultRef       string `gorm:"column:vault_ref;type:varchar(160);not null;uniqueIndex:uk_site_credential_owner_vault" json:"-"`
	Enabled        bool   `gorm:"column:enabled;type:boolean;not null;default:true" json:"enabled"`
}

func (p *SiteCredential) BeforeCreate(tx *gorm.DB) error {
	if p.Ulid == "" {
		p.Ulid = ulid.New()
	}
	return nil
}

func (*SiteCredential) TableName() string { return "sys_site_credential" }
