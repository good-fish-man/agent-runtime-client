// Package user holds user-domain entities.
package user

// SysUser is the user domain entity mapped from sys_user.
type SysUser struct {
	Ulid       string `json:"ulid"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	DeletedAt  int64  `json:"deleted_at"`
	CreatedBy  string `json:"created_by"`
	UpdatedBy  string `json:"updated_by"`
	DeletedBy  string `json:"deleted_by"`
	MemberCode string `json:"member_code"`
	Phone      string `json:"phone"`
	LevelId    string `json:"level_id"`
	NickName   string `json:"nick_name"`
	AvatarURL  string `json:"avatar_url"`
	TrueName   string `json:"true_name"`
	State      uint   `json:"state"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	AdminLevel uint   `json:"admin_level"`
	DepId      string `json:"dep_id"`
	JobId      string `json:"job_id"`
	RoleId     string `json:"role_id"`
	Unionid    string `json:"unionid"`
	LoginIp    string `json:"login_ip"`
}
