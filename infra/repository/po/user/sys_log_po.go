package user

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// SysLog is the gorm persistence object mapped to table sys_log.
type SysLog struct {
	Ulid      string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
	UpdatedAt int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint;comment:修改时间;" json:"updated_at"`
	DeletedAt int64  `gorm:"column:deleted_at;type:bigint;comment:删除时间;" json:"deleted_at"`
	CreatedBy string `gorm:"column:created_by;type:varchar(32);comment:创建者;" json:"created_by"`
	UpdatedBy string `gorm:"column:updated_by;type:varchar(32);comment:修改者;" json:"updated_by"`
	DeletedBy string `gorm:"column:deleted_by;type:varchar(32);comment:删除者;" json:"deleted_by"`
	Msg       string `gorm:"column:msg;type:varchar(255);uniqueIndex;comment:msg;" json:"msg"`
}

// BeforeCreate assigns a ULID primary key when absent.
func (po *SysLog) BeforeCreate(tx *gorm.DB) (err error) {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return
}

// TableName maps to sys_log.
func (po *SysLog) TableName() string { return "sys_log" }
