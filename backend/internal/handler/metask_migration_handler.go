package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// MetaskMigrationHandler 处理「迁移到 metask-ai」。
type MetaskMigrationHandler struct {
	migrationService *service.MetaskMigrationService
}

// NewMetaskMigrationHandler 创建 MetaskMigrationHandler。
func NewMetaskMigrationHandler(migrationService *service.MetaskMigrationService) *MetaskMigrationHandler {
	return &MetaskMigrationHandler{migrationService: migrationService}
}

// GetStatus 返回当前用户的迁移状态与将要迁移的余额 / 订阅 / Key 数。
// GET /api/v1/user/migration
func (h *MetaskMigrationHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.migrationService.GetStatus(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// Migrate 用户确认迁移：冻结本站账号并在 metask-ai 入账。可重复调用。
// POST /api/v1/user/migration
func (h *MetaskMigrationHandler) Migrate(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.migrationService.Migrate(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}
