// Package user provides the application service orchestrating SysUser use cases.
package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/user"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/user"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/user"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	userpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	log "github.com/good-fish-man/logx"
)

// SysUserService is the application service for users.
type SysUserService struct {
	asm    *assembler.SysUserDto
	userSv *srv.SysUserSvc
	logSvc *srv.SysLogSvc
	data   *data.Data
}

// NewSysUserService wires the service over the shared data handle.
func NewSysUserService(d *data.Data) *SysUserService {
	return &SysUserService{
		asm:    assembler.NewSysUserDto(),
		userSv: srv.NewSysUserSvc(d),
		logSvc: srv.NewSysLogSvc(d),
		data:   d,
	}
}

func (s *SysUserService) CreateSysUser(ctx context.Context, req *dto.CreateSysUserReq) (*dto.CreateSysUserRsp, error) {
	if strings.TrimSpace(req.Password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, log.WrapError(err, "SysUserService.CreateSysUser.hashPassword")
		}
		req.Password = string(hash)
	}
	en := s.asm.D2ECreateSysUser(req)

	if en.NickName != "" {
		existing, err := s.userSv.FindSysUserByQuery(ctx, []*query.Query{{Key: "nick_name", Value: en.NickName, Operator: query.OpEq}})
		if err != nil {
			return nil, log.WrapError(err, "SysUserService.CreateSysUser.findNickname")
		}
		if existing != nil && existing.Ulid != "" && existing.DeletedAt == 0 {
			return nil, apierror.ErrBadRequest.WithMessage(fmt.Sprintf("the nick_name: %s has", en.NickName))
		}
	}

	ulid, err := s.userSv.CreateSysUser(ctx, en)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.CreateSysUser.create")
	}
	return &dto.CreateSysUserRsp{Ulid: ulid}, nil
}

// Register creates a normal user account. Administrative fields are never accepted from this endpoint.
func (s *SysUserService) Register(ctx context.Context, req *dto.RegisterReq) (*dto.LoginRsp, error) {
	username := strings.TrimSpace(req.UserName)
	if len(username) < 3 || len(req.Password) < 6 {
		return nil, apierror.ErrBadRequest.WithMessage("用户名至少 3 个字符，密码至少 6 个字符")
	}
	existing, err := s.userSv.FindSysUserByQuery(ctx, []*query.Query{{Key: "member_code", Value: username, Operator: query.OpEq}})
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.Register.findExisting")
	}
	if existing != nil && existing.Ulid != "" && existing.DeletedAt == 0 {
		return nil, apierror.ErrBadRequest.WithMessage("用户名已存在")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.Register.hashPassword")
	}
	nickname := strings.TrimSpace(req.NickName)
	if nickname == "" {
		nickname = username
	}
	user := &entity.SysUser{MemberCode: username, NickName: nickname, Password: string(hash), State: 1}
	userID, err := s.userSv.CreateSysUser(ctx, user)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.Register.create")
	}
	user.Ulid = userID
	return s.issueSession(ctx, user)
}

func (s *SysUserService) Login(ctx context.Context, req *dto.LoginReq) (*dto.LoginRsp, error) {
	username := strings.TrimSpace(req.UserName)
	user, err := s.userSv.FindSysUserByQuery(ctx, []*query.Query{
		{Key: "member_code", Value: username, Operator: query.OpEq},
		{Key: "deleted_at", Value: 0, Operator: query.OpEq},
	})
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.Login.findUser")
	}
	if user == nil || user.Ulid == "" || user.State != 1 || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		return nil, apierror.ErrUnauthorized.WithMessage("用户名或密码错误")
	}
	return s.issueSession(ctx, user)
}

func (s *SysUserService) issueSession(ctx context.Context, user *entity.SysUser) (*dto.LoginRsp, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, log.WrapError(err, "SysUserService.issueSession.generateToken")
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	session := &userpo.SysUserSession{UserID: user.Ulid, TokenHash: middleware.TokenHash(token), ExpiresAt: expiresAt.UnixMilli()}
	if err := s.data.DB(ctx).Create(session).Error; err != nil {
		return nil, log.WrapError(err, "SysUserService.issueSession.createSession")
	}
	return &dto.LoginRsp{
		AccessToken: token,
		ExpiresIn:   int(time.Until(expiresAt).Seconds()),
		TokenType:   "Bearer",
		Scope:       "user",
		User:        s.asm.E2DFindSysUserRsp(user),
	}, nil
}

func (s *SysUserService) Logout(ctx context.Context, tokenHash string) error {
	return log.WrapError(s.data.DB(ctx).Model(&userpo.SysUserSession{}).Where("token_hash = ?", tokenHash).Update("revoked_at", time.Now().UnixMilli()).Error, "SysUserService.Logout")
}

func (s *SysUserService) DeleteSysUser(ctx context.Context, req *dto.DelSysUsersReq) error {
	en := s.asm.D2EDeleteSysUser(req)
	if err := s.userSv.DeleteSysUser(ctx, en); err != nil {
		return log.WrapError(err, "SysUserService.DeleteSysUser")
	}
	if _, err := s.logSvc.CreateSysLog(ctx, &entity.SysLog{CreatedBy: req.DeletedBy, Msg: "SysUser.Delete"}); err != nil {
		log.WarnwCtx(ctx, "audit log write failed", "operation", "SysUser.Delete", "error_chain", log.FormatError(log.WrapError(err, "SysUserService.DeleteSysUser.audit")))
	}
	return nil
}

func (s *SysUserService) UpdateSysUser(ctx context.Context, req *dto.UpdateSysUserReq) error {
	en := s.asm.D2EUpdateSysUser(req)
	if err := s.userSv.UpdateSysUser(ctx, en); err != nil {
		return log.WrapError(err, "SysUserService.UpdateSysUser")
	}
	if _, err := s.logSvc.CreateSysLog(ctx, &entity.SysLog{CreatedBy: req.UpdatedBy, Msg: "SysUser.Update"}); err != nil {
		log.WarnwCtx(ctx, "audit log write failed", "operation", "SysUser.Update", "error_chain", log.FormatError(log.WrapError(err, "SysUserService.UpdateSysUser.audit")))
	}
	return nil
}

// UpdateAvatar updates only the authenticated user's avatar and returns the
// refreshed public user representation.
func (s *SysUserService) UpdateAvatar(ctx context.Context, userID, avatarURL string) (*dto.FindSysUserRsp, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(avatarURL) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("用户或头像地址不能为空")
	}
	if err := s.userSv.UpdateSysUser(ctx, &entity.SysUser{Ulid: userID, UpdatedBy: userID, AvatarURL: avatarURL}); err != nil {
		return nil, log.WrapError(err, "SysUserService.UpdateAvatar")
	}
	return s.FindSysUserById(ctx, &dto.FindSysUserByIdReq{Ulid: userID})
}

func (s *SysUserService) FindSysUserById(ctx context.Context, req *dto.FindSysUserByIdReq) (*dto.FindSysUserRsp, error) {
	en, err := s.userSv.FindSysUserById(ctx, req.Ulid)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.FindSysUserById")
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage(fmt.Sprintf("user_id: %s, info not found", req.Ulid))
	}
	return s.asm.E2DFindSysUserRsp(en), nil
}

func (s *SysUserService) FindSysUserByQuery(ctx context.Context, req *dto.FindSysUserByQueryReq) (*dto.FindSysUserRsp, error) {
	en, err := s.userSv.FindSysUserByQuery(ctx, req.Query)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.FindSysUserByQuery")
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("user not found")
	}
	return s.asm.E2DFindSysUserRsp(en), nil
}

func (s *SysUserService) FindSysUserAll(ctx context.Context, req *dto.FindSysUserAllReq) ([]*dto.FindSysUserRsp, error) {
	ens, err := s.userSv.FindSysUserAll(ctx, req.Query)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.FindSysUserAll")
	}
	return s.asm.E2DGetSysUsers(ens), nil
}

func (s *SysUserService) FindSysUserPage(ctx context.Context, req *dto.FindSysUserPageReq) (*dto.FindSysUserPageRsp, error) {
	ens, pageData, err := s.userSv.FindSysUserPage(ctx, req.Query, req.PageData, req.SortData)
	if err != nil {
		return nil, log.WrapError(err, "SysUserService.FindSysUserPage")
	}
	return &dto.FindSysUserPageRsp{Entries: s.asm.E2DGetSysUsers(ens), PageData: pageData}, nil
}
