package handler

import (
	"log"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/go-pay/gopay"
)

type AlipayHandler struct {
	alipayService *service.AlipayService
}

func NewAlipayHandler(alipayService *service.AlipayService) *AlipayHandler {
	return &AlipayHandler{alipayService: alipayService}
}

type alipayCreateOrderRequest struct {
	PackageID int     `json:"package_id"`
	CnyAmount float64 `json:"cny_amount"` // 自定义金额（元），与 package_id 二选一
}

// GetPackages 获取充值套餐列表
// GET /api/v1/payments/alipay/packages
func (h *AlipayHandler) GetPackages(c *gin.Context) {
	pkgs, err := h.alipayService.GetPackages(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, pkgs)
}

// CreateOrder 创建支付宝当面付订单，返回二维码链接
// POST /api/v1/payments/alipay/create-order
func (h *AlipayHandler) CreateOrder(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req alipayCreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.PackageID == 0 && req.CnyAmount <= 0 {
		response.BadRequest(c, "package_id or cny_amount is required")
		return
	}

	order, err := h.alipayService.CreateOrder(c.Request.Context(), subject.UserID, req.PackageID, req.CnyAmount)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	qrCode := ""
	if order.QRCode != nil {
		qrCode = *order.QRCode
	}

	response.Success(c, gin.H{
		"order_no":   order.OrderNo,
		"qr_code":    qrCode,
		"expires_at": order.ExpiresAt,
	})
}

// GetOrderStatus 查询订单状态（前端轮询）
// GET /api/v1/payments/alipay/order/:order_no
func (h *AlipayHandler) GetOrderStatus(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	orderNo := c.Param("order_no")
	if orderNo == "" {
		response.BadRequest(c, "order_no is required")
		return
	}

	order, err := h.alipayService.GetOrderStatus(c.Request.Context(), subject.UserID, orderNo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"order_no":   order.OrderNo,
		"status":     order.Status,
		"cny_fee":    order.CnyFee,
		"usd_amount": order.UsdAmount,
		"paid_at":    order.PaidAt,
		"expires_at": order.ExpiresAt,
		"created_at": order.CreatedAt,
	})
}

// HandleNotify 接收支付宝异步通知
// POST /api/v1/payments/alipay/notify
func (h *AlipayHandler) HandleNotify(c *gin.Context) {
	ctx := c.Request.Context()
	clientIP := c.ClientIP()

	// 解析 form 数据
	if err := c.Request.ParseForm(); err != nil {
		log.Printf("alipay notify: parse form failed: ip=%s err=%v", clientIP, err)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	notifyMap := make(gopay.BodyMap)
	for k, v := range c.Request.PostForm {
		if len(v) > 0 {
			notifyMap[k] = v[0]
		}
	}

	tradeStatus := notifyMap.GetString("trade_status")
	outTradeNo := notifyMap.GetString("out_trade_no")
	signType := notifyMap.GetString("sign_type")

	ok, err := h.alipayService.VerifyNotifySign(ctx, notifyMap)
	if err != nil || !ok {
		log.Printf("alipay notify: verify sign failed: ip=%s out_trade_no=%s trade_status=%s sign_type=%s err=%v ok=%v",
			clientIP, outTradeNo, tradeStatus, signType, err, ok)
		c.String(http.StatusBadRequest, "fail")
		return
	}
	log.Printf("alipay notify: verify sign ok: ip=%s trade_status=%s out_trade_no=%s sign_type=%s",
		clientIP, tradeStatus, outTradeNo, signType)

	updated, err := h.alipayService.HandleNotify(ctx, notifyMap)
	if err != nil {
		log.Printf("alipay notify: handle failed: out_trade_no=%s trade_status=%s err=%v", outTradeNo, tradeStatus, err)
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	if !updated {
		log.Printf("alipay notify: duplicate or non-pending, skipped: out_trade_no=%s trade_status=%s", outTradeNo, tradeStatus)
	}

	c.String(http.StatusOK, "success")
}

// GetOrders 获取用户充值记录
// GET /api/v1/payments/alipay/orders
func (h *AlipayHandler) GetOrders(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	orders, result, err := h.alipayService.ListOrdersByUser(
		c.Request.Context(), subject.UserID,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Paginated(c, orders, result.Total, page, pageSize)
}
