package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	userpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Auth validates a revocable opaque Bearer token and binds its user to Gin and request contexts.
func Auth(store *data.Data) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			response.Err(c, apierror.ErrUnauthorized.WithMessage("请先登录"))
			c.Abort()
			return
		}
		hash := TokenHash(strings.TrimSpace(parts[1]))
		var session userpo.SysUserSession
		err := store.DB(c.Request.Context()).Where("token_hash = ? AND revoked_at = 0 AND expires_at > ?", hash, time.Now().UnixMilli()).First(&session).Error
		if err != nil {
			response.Err(c, apierror.ErrUnauthorized.WithMessage("登录已失效，请重新登录"))
			c.Abort()
			return
		}
		var user userpo.SysUser
		if err := store.DB(c.Request.Context()).Where("ulid = ? AND deleted_at = 0 AND state = 1", session.UserID).First(&user).Error; err != nil {
			response.Err(c, apierror.ErrUnauthorized.WithMessage("用户不存在或已禁用"))
			c.Abort()
			return
		}
		c.Set(consts.CtxKeyUserID, session.UserID)
		c.Set(consts.CtxKeyAdminLevel, user.AdminLevel)
		c.Set(consts.CtxKeyTokenHash, hash)
		c.Request = c.Request.WithContext(authctx.WithUserID(c.Request.Context(), session.UserID))
		c.Next()
	}
}
