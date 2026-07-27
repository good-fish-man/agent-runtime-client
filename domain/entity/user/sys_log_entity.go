package user

// SysLog is the sys_log domain entity.
type SysLog struct {
	Ulid      string `json:"ulid"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at"`
	CreatedBy string `json:"created_by"`
	UpdatedBy string `json:"updated_by"`
	DeletedBy string `json:"deleted_by"`
	Msg       string `json:"msg"`
}
