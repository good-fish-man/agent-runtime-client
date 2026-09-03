package user

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	CreateSysUserReq struct {
		CreatedBy  string              `json:"created_by"`
		MemberCode string              `json:"member_code" validate:"required"`
		Phone      string              `json:"phone"`
		NickName   string              `json:"nick_name"`
		TrueName   string              `json:"true_name"`
		LevelId    string              `json:"level_id"`
		DepId      string              `json:"organization_id"`
		Password   string              `json:"password"`
		AdminLevel uint                `json:"admin_level"`
		Exp        CreateSysUserReqExp `json:"exp"`
	}

	CreateSysUserReqExp struct {
		Addr     string `json:"addr"`
		AddrCode string `json:"addr_code"`
	}

	DelSysUsersReq struct {
		Ulid      string `validate:"required" uri:"ulid" json:"ulid"`
		DeletedBy string `json:"deleted_by"`
	}

	UpdateSysUserReq struct {
		Ulid       string `validate:"required" uri:"ulid" json:"ulid"`
		UpdatedBy  string `json:"updated_by"`
		MemberCode string `json:"member_code"`
		Phone      string `json:"phone"`
		NickName   string `json:"nick_name"`
		AvatarURL  string `json:"avatar_url"`
		Unionid    string `json:"unionid"`
		LevelId    string `json:"level_id"`
		DepId      string `json:"organization_id"`
	}

	FindSysUserByIdReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	FindSysUserByQueryReq struct {
		Query []*query.Query `json:"query"`
	}

	FindSysUserAllReq struct {
		Query []*query.Query `json:"query"`
	}

	FindSysUserPageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
	}

	LoginReq struct {
		UserName string `validate:"required" json:"username"`
		Password string `validate:"required" json:"password"`
		LoginIp  string `json:"login_ip"`
	}

	RegisterReq struct {
		UserName string `validate:"required" json:"username"`
		Password string `validate:"required" json:"password"`
		NickName string `json:"nickname"`
	}

	LogoutReq struct {
		Token string `json:"token"`
	}
)

// Response DTOs.
type (
	LoginRsp struct {
		AccessToken  string          `json:"access_token"`
		ExpiresIn    int             `json:"expires_in"`
		TokenType    string          `json:"token_type"`
		RefreshToken string          `json:"refresh_token"`
		Scope        string          `json:"scope"`
		UserExp      string          `json:"user_exp"`
		User         *FindSysUserRsp `json:"user"`
	}

	CreateSysUserRsp struct {
		Ulid string `json:"ulid"`
	}

	FindSysUserPageRsp struct {
		Entries  []*FindSysUserRsp `json:"entries"`
		PageData *query.PageData   `json:"page_data"`
	}

	FindSysUserRsp struct {
		Ulid       string              `json:"ulid"`
		CreatedAt  int64               `json:"created_at"`
		UpdatedAt  int64               `json:"updated_at"`
		CreatedBy  string              `json:"created_by"`
		UpdatedBy  string              `json:"updated_by"`
		MemberCode string              `json:"member_code"`
		Phone      string              `json:"phone"`
		NickName   string              `json:"nick_name"`
		AvatarURL  string              `json:"avatar_url"`
		Unionid    string              `json:"unionid"`
		LevelId    string              `json:"level_id"`
		DepId      string              `json:"organization_id"`
		AdminLevel uint                `json:"admin_level"`
		Exp        CreateSysUserRspExp `json:"exp"`
	}

	CreateSysUserRspExp struct {
		Addr     string `json:"addr"`
		AddrCode string `json:"addr_code"`
	}
)
