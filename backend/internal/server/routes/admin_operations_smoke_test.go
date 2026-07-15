package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestRegisterUserManagementRoutes_OperationsReportNoConflict 真实注册 admin 用户管理路由，
// 验证静态段 /users/operations-report 与 /users/:id 共存不触发 gin 路由冲突 panic，
// 且请求会被路由到 operations handler（而不是被 :id 吞掉）。
func TestRegisterUserManagementRoutes_OperationsReportNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	h := &handler.Handlers{
		Admin: &handler.AdminHandlers{
			User:           admin.NewUserHandler(nil, nil),
			UserAttribute:  admin.NewUserAttributeHandler(nil),
			UserOperations: admin.NewUserOperationsHandler(nil),
		},
	}
	passAuth := middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() })

	adminGroup := v1.Group("/admin")
	adminGroup.Use(gin.HandlerFunc(passAuth))
	require.NotPanics(t, func() {
		registerUserManagementRoutes(adminGroup, h)
	})

	// 缺参数应命中 operations handler 的 400（若被 /:id 吞掉则会是其它行为）
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/operations-report", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
