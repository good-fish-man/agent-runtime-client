package user

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// SysUserSession stores revocable opaque login tokens. Only token hashes are persisted.
type SysUserSession struct {
	Ulid      string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	UserID    string `gorm:"column:user_id;type:varchar(128);not null;index" json:"user_id"`
	TokenHash string `gorm:"column:token_hash;type:varchar(64);not null;uniqueIndex" json:"-"`
	ExpiresAt int64  `gorm:"column:expires_at;type:bigint;not null;index" json:"expires_at"`
	RevokedAt int64  `gorm:"column:revoked_at;type:bigint;default:0" json:"revoked_at"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
}

func (po *SysUserSession) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (*SysUserSession) TableName() string { return "sys_user_session" }
