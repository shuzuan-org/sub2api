package admin

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AlipayHandler handles admin alipay management
type AlipayHandler struct {
	alipayService *service.AlipayService
}

func NewAlipayHandler(alipayService *service.AlipayService) *AlipayHandler {
	return &AlipayHandler{alipayService: alipayService}
}

// GetConfig 获取支付宝配置（屏蔽敏感字段，仅返回是否已设置）
// GET /api/v1/admin/alipay/config
func (h *AlipayHandler) GetConfig(c *gin.Context) {
	ctx := c.Request.Context()
	notifyURL, _ := h.alipayService.NotifyURL(ctx)
	enabled := h.alipayService.IsEnabled(ctx)

	cfg, err := h.alipayService.GetConfig(ctx)
	if err != nil {
		response.Success(c, gin.H{
			"mode":       service.AlipayModePublicKey,
			"notify_url": notifyURL,
			"enabled":    enabled,
			"configured": false,
		})
		return
	}
	response.Success(c, gin.H{
		"mode":                   cfg.Mode,
		"app_id":                 cfg.AppID,
		"seller_id":              cfg.SellerID,
		"notify_url":             notifyURL,
		"enabled":                enabled,
		"is_prod":                cfg.IsProd,
		"private_key_set":        cfg.PrivateKey != "",
		"public_key_set":         cfg.PublicKey != "",
		"app_public_cert_set":    cfg.AppPublicCert != "",
		"alipay_public_cert_set": cfg.AlipayPublicCert != "",
		"alipay_root_cert_set":   cfg.AlipayRootCert != "",
		"configured":             true,
	})
}

type alipayUpdateConfigRequest struct {
	Mode             string `json:"mode"`
	AppID            string `json:"app_id"`
	SellerID         string `json:"seller_id"`
	PrivateKey       string `json:"private_key"`
	PublicKey        string `json:"public_key"`
	AppPublicCert    string `json:"app_public_cert"`
	AlipayPublicCert string `json:"alipay_public_cert"`
	AlipayRootCert   string `json:"alipay_root_cert"`
	IsProd           bool   `json:"is_prod"`
}

// UpdateConfig 更新支付宝配置
// PUT /api/v1/admin/alipay/config
func (h *AlipayHandler) UpdateConfig(c *gin.Context) {
	var req alipayUpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	unescape := func(s string) string { return strings.ReplaceAll(s, `\n`, "\n") }
	cfg := &service.AlipayConfig{
		Mode:             req.Mode,
		AppID:            req.AppID,
		SellerID:         req.SellerID,
		PrivateKey:       unescape(req.PrivateKey),
		PublicKey:        unescape(req.PublicKey),
		AppPublicCert:    unescape(req.AppPublicCert),
		AlipayPublicCert: unescape(req.AlipayPublicCert),
		AlipayRootCert:   unescape(req.AlipayRootCert),
		IsProd:           req.IsProd,
	}
	if err := h.alipayService.UpdateConfig(c.Request.Context(), cfg); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

type alipayUpdateEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// SetEnabled 启用/禁用支付宝支付
// PUT /api/v1/admin/alipay/enabled
func (h *AlipayHandler) SetEnabled(c *gin.Context) {
	var req alipayUpdateEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.alipayService.SetEnabled(c.Request.Context(), req.Enabled); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

// GetPackages 获取充值套餐配置
// GET /api/v1/admin/alipay/packages
func (h *AlipayHandler) GetPackages(c *gin.Context) {
	pkgs, err := h.alipayService.GetPackages(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, pkgs)
}

// UpdatePackages 覆盖保存充值套餐配置
// PUT /api/v1/admin/alipay/packages
func (h *AlipayHandler) UpdatePackages(c *gin.Context) {
	var pkgs []service.AlipayPackage
	if err := c.ShouldBindJSON(&pkgs); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.alipayService.SavePackages(c.Request.Context(), pkgs); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

// ListOrders 查询订单列表
// GET /api/v1/admin/alipay/orders
func (h *AlipayHandler) ListOrders(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := c.Query("status")

	orders, result, err := h.alipayService.ListOrders(
		c.Request.Context(),
		pagination.PaginationParams{Page: page, PageSize: pageSize},
		status,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, orders, result.Total, page, pageSize)
}
