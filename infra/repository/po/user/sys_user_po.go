package user

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// SysUser is the gorm persistence object mapped to table sys_user.
type SysUser struct {
	Ulid       string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	CreatedAt  int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
	UpdatedAt  int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint;comment:修改时间;" json:"updated_at"`
	DeletedAt  int64  `gorm:"column:deleted_at;type:bigint;comment:删除时间;" json:"deleted_at"`
	CreatedBy  string `gorm:"column:created_by;type:varchar(128);comment:创建者;" json:"created_by"`
	UpdatedBy  string `gorm:"column:updated_by;type:varchar(128);comment:修改者;" json:"updated_by"`
	DeletedBy  string `gorm:"column:deleted_by;type:varchar(128);comment:删除者;" json:"deleted_by"`
	MemberCode string `gorm:"column:member_code;type:varchar(32);comment:会员号;" json:"member_code"`
	Phone      string `gorm:"column:phone;type:varchar(20);comment:手机号码;" json:"phone"`
	LevelId    string `gorm:"column:level_id;type:varchar(128);default:0;comment:会员等级id;" json:"level_id"`
	NickName   string `gorm:"column:nick_name;type:varchar(64);comment:昵称;" json:"nick_name"`
	AvatarURL  string `gorm:"column:avatar_url;type:varchar(512);comment:用户头像地址;" json:"avatar_url"`
	TrueName   string `gorm:"column:true_name;type:varchar(64);comment:真实姓名;" json:"true_name"`
	State      uint   `gorm:"column:state;type:int;default:1;comment:1显示,2否;" json:"state"`
	Email      string `gorm:"column:email;type:varchar(255);comment:邮箱;" json:"email"`
	Password   string `gorm:"column:password;type:varchar(2000);comment:密码;" json:"password"`
	AdminLevel uint   `gorm:"column:admin_level;type:int;default:0;comment:超管级别;" json:"admin_level"`
	DepId      string `gorm:"column:dep_id;type:varchar(128);comment:部门id;" json:"dep_id"`
	JobId      string `gorm:"column:job_id;type:varchar(128);comment:职位id;" json:"job_id"`
	RoleId     string `gorm:"column:role_id;type:varchar(128);comment:角色id;" json:"role_id"`
}

// BeforeCreate assigns a ULID primary key when absent.
func (po *SysUser) BeforeCreate(tx *gorm.DB) (err error) {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return
}

// TableName maps to sys_user.
func (po *SysUser) TableName() string { return "sys_user" }
