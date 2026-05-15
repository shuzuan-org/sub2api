package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/alipay"
)

// ---- 错误定义 ----

var (
	ErrAlipayOrderNotFound  = infraerrors.NotFound("ALIPAY_ORDER_NOT_FOUND", "order not found")
	ErrAlipayNotEnabled     = infraerrors.BadRequest("ALIPAY_NOT_ENABLED", "alipay is not enabled")
	ErrAlipayNotConfigured  = infraerrors.BadRequest("ALIPAY_NOT_CONFIGURED", "alipay is not configured")
	ErrAlipayInvalidPackage = infraerrors.BadRequest("ALIPAY_INVALID_PACKAGE", "invalid payment package")
)

// ---- Setting 键名常量 ----

const (
	SettingKeyAlipayConfig   = "alipay_config"
	SettingKeyAlipayEnabled  = "alipay_enabled"
	SettingKeyAlipayPackages = "alipay_packages"
)

// ---- 数据模型 ----

// AlipayPackage 充值套餐（存 Setting 表）
type AlipayPackage struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	CnyAmount float64 `json:"cny_amount"` // 人民币金额（元）
	UsdAmount float64 `json:"usd_amount"` // 到账 U 代币
}

// AlipayOrder 支付宝订单
type AlipayOrder struct {
	ID            int64      `json:"id"`
	OrderNo       string     `json:"order_no"`
	UserID        int64      `json:"user_id"`
	PackageID     int        `json:"package_id"`
	CnyFee        int        `json:"cny_fee"`    // 人民币金额（分）
	UsdAmount     float64    `json:"usd_amount"` // 到账金额（U 代币，字段名保留历史兼容）
	Status        string     `json:"status"`     // pending / paid / expired / refunded
	AlipayTradeNo *string    `json:"alipay_trade_no"`
	QRCode        *string    `json:"-"`
	ExpiresAt     time.Time  `json:"expires_at"`
	PaidAt        *time.Time `json:"paid_at"`
	NotifyData    *string    `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// AlipayMode 支付宝验签模式
const (
	AlipayModePublicKey = "public_key" // 公钥模式：私钥 + 支付宝公钥
	AlipayModeCert      = "cert"       // 证书模式：私钥 + 应用公钥证书 + 支付宝公钥证书 + 支付宝根证书
)

// AlipayConfig 支付宝支付配置（存 Setting 表）
type AlipayConfig struct {
	Mode       string `json:"mode"` // public_key（默认）/ cert
	AppID      string `json:"app_id"`
	SellerID   string `json:"seller_id"`   // 支付宝收款账号 PID（可选）
	PrivateKey string `json:"private_key"` // 应用私钥（PKCS1 或 PKCS8 PEM，或裸 base64）
	IsProd     bool   `json:"is_prod"`     // true=正式环境，false=沙箱

	// 公钥模式
	PublicKey string `json:"public_key"` // 支付宝公钥（裸 base64 或 PEM）

	// 证书模式（PEM 文本）
	AppPublicCert    string `json:"app_public_cert"`    // 应用公钥证书 appCertPublicKey_xxxx.crt
	AlipayPublicCert string `json:"alipay_public_cert"` // 支付宝公钥证书 alipayCertPublicKey_RSA2.crt
	AlipayRootCert   string `json:"alipay_root_cert"`   // 支付宝根证书 alipayRootCert.crt
}

// modeOrDefault 返回有效的验签模式，空值视为公钥模式（向后兼容旧配置）
func (c *AlipayConfig) modeOrDefault() string {
	if c.Mode == AlipayModeCert {
		return AlipayModeCert
	}
	return AlipayModePublicKey
}

// ---- Repository 接口 ----

type AlipayOrderRepository interface {
	Create(ctx context.Context, order *AlipayOrder) error
	GetByOrderNo(ctx context.Context, orderNo string) (*AlipayOrder, error)
	// MarkPaid 幂等标记支付成功，返回 true 表示本次更新生效
	MarkPaid(ctx context.Context, orderNo, alipayTradeNo, notifyData string) (bool, error)
	ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams) ([]AlipayOrder, *pagination.PaginationResult, error)
	List(ctx context.Context, params pagination.PaginationParams, status string) ([]AlipayOrder, *pagination.PaginationResult, error)
}

// ---- Service ----

type AlipayService struct {
	db          *dbent.Client
	cfg         *config.Config
	orderRepo   AlipayOrderRepository
	settingRepo SettingRepository
	userService *UserService

	// 缓存已构建的 alipay client，避免每次创建订单都重新解析私钥/证书。
	// cacheKey 由 getOrBuildClient 基于配置全字段计算，任何字段变更都会重建。
	clientMu       sync.Mutex
	clientCacheKey string
	cachedClient   *alipay.Client
}

func NewAlipayService(
	db *dbent.Client,
	cfg *config.Config,
	orderRepo AlipayOrderRepository,
	settingRepo SettingRepository,
	userService *UserService,
) *AlipayService {
	return &AlipayService{
		db:          db,
		cfg:         cfg,
		orderRepo:   orderRepo,
		settingRepo: settingRepo,
		userService: userService,
	}
}

// NotifyURL 生成支付宝回调地址
func (s *AlipayService) NotifyURL(ctx context.Context) (string, bool) {
	base := s.cfg.Server.FrontendURL
	if val, err := s.settingRepo.GetValue(ctx, SettingKeyFrontendURL); err == nil && strings.TrimSpace(val) != "" {
		base = strings.TrimSpace(val)
	}
	base = strings.TrimRight(base, "/")
	u := base + "/api/v1/payments/alipay/notify"
	valid := strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
	return u, valid
}

// cert 目录中约定的文件名
const (
	certFileAppPrivateKey  = "appPrivateKey.pem"
	certFileAppPublicCert  = "appPublicCert.crt"
	certFileAlipayPubCert  = "alipayPublicCert.crt"
	certFileAlipayRootCert = "alipayRootCert.crt"
)

// GetConfig 获取支付宝配置。
// 优先级：cert 目录（4 个文件齐全且 config.yaml 配了 app_id）> config.yaml（公钥模式）> Setting 表。
func (s *AlipayService) GetConfig(ctx context.Context) (*AlipayConfig, error) {
	// 1. cert 目录：appPrivateKey.pem + appPublicCert.crt + alipayPublicCert.crt + alipayRootCert.crt 齐全
	if cfg, ok := s.loadConfigFromCertDir(); ok {
		return cfg, nil
	}

	// 2. config.yaml 公钥模式：AppID 非空即视为已配置
	if s.cfg.Alipay.AppID != "" {
		if s.cfg.Alipay.PrivateKey == "" || s.cfg.Alipay.PublicKey == "" {
			return nil, fmt.Errorf("alipay config in config.yaml is incomplete: missing private_key or public_key (or provide certificate files under %s)", s.certDir())
		}
		return &AlipayConfig{
			Mode:       AlipayModePublicKey,
			AppID:      s.cfg.Alipay.AppID,
			SellerID:   s.cfg.Alipay.SellerID,
			PrivateKey: s.cfg.Alipay.PrivateKey,
			PublicKey:  s.cfg.Alipay.PublicKey,
			IsProd:     s.cfg.Alipay.IsProd,
		}, nil
	}

	// 3. 回落到 Setting 表（管理后台手动配置）
	val, err := s.settingRepo.GetValue(ctx, SettingKeyAlipayConfig)
	if err != nil {
		return nil, ErrAlipayNotConfigured
	}
	var cfg AlipayConfig
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		return nil, ErrAlipayNotConfigured
	}
	cfg.Mode = cfg.modeOrDefault()
	return &cfg, nil
}

// certDir 返回证书目录路径（默认 ./cert）
func (s *AlipayService) certDir() string {
	dir := strings.TrimSpace(s.cfg.Alipay.CertDir)
	if dir == "" {
		return "./cert"
	}
	return dir
}

// loadConfigFromCertDir 尝试从证书目录构造证书模式配置；
// 4 个文件齐全 + config.yaml 配了 app_id 时返回 (cfg, true)，否则 (nil, false)。
func (s *AlipayService) loadConfigFromCertDir() (*AlipayConfig, bool) {
	if s.cfg.Alipay.AppID == "" {
		return nil, false
	}
	dir := s.certDir()
	read := func(name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", false
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", false
		}
		return v, true
	}
	priv, ok1 := read(certFileAppPrivateKey)
	appCert, ok2 := read(certFileAppPublicCert)
	alipayCert, ok3 := read(certFileAlipayPubCert)
	rootCert, ok4 := read(certFileAlipayRootCert)
	if !(ok1 && ok2 && ok3 && ok4) {
		return nil, false
	}
	return &AlipayConfig{
		Mode:             AlipayModeCert,
		AppID:            s.cfg.Alipay.AppID,
		SellerID:         s.cfg.Alipay.SellerID,
		IsProd:           s.cfg.Alipay.IsProd,
		PrivateKey:       priv,
		AppPublicCert:    appCert,
		AlipayPublicCert: alipayCert,
		AlipayRootCert:   rootCert,
	}, true
}

// getOrBuildClient 返回缓存的 alipay client。
// cache key = sha256(mode|appID|privateKey|isProd|publicKey|appPublicCert|alipayPublicCert|alipayRootCert)[:8]，任意字段变更都会触发重建。
func (s *AlipayService) getOrBuildClient(cfg *AlipayConfig) (*alipay.Client, error) {
	isProdStr := "0"
	if cfg.IsProd {
		isProdStr = "1"
	}
	keyParts := strings.Join([]string{
		cfg.modeOrDefault(), cfg.AppID, cfg.PrivateKey, isProdStr,
		cfg.PublicKey, cfg.AppPublicCert, cfg.AlipayPublicCert, cfg.AlipayRootCert,
	}, "|")
	h := sha256.Sum256([]byte(keyParts))
	key := fmt.Sprintf("%x", h[:8])

	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if s.cachedClient != nil && s.clientCacheKey == key {
		return s.cachedClient, nil
	}
	client, err := buildAlipayClient(cfg)
	if err != nil {
		return nil, err
	}
	s.cachedClient = client
	s.clientCacheKey = key
	return client, nil
}

// SaveConfig 保存支付宝配置，同时失效 client 缓存
func (s *AlipayService) SaveConfig(ctx context.Context, cfg *AlipayConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyAlipayConfig, string(b)); err != nil {
		return err
	}
	// config 变更，清空缓存的 client
	s.clientMu.Lock()
	s.cachedClient = nil
	s.clientCacheKey = ""
	s.clientMu.Unlock()
	return nil
}

// UpdateConfig 更新配置；私钥/公钥/证书等敏感字段为空时保留已存储的值
func (s *AlipayService) UpdateConfig(ctx context.Context, incoming *AlipayConfig) error {
	incoming.Mode = incoming.modeOrDefault()

	existing, err := s.GetConfig(ctx)
	if err != nil {
		existing = nil
	}
	keep := func(in *string, old string) {
		if *in == "" {
			*in = old
		}
	}
	if existing != nil {
		keep(&incoming.PrivateKey, existing.PrivateKey)
		keep(&incoming.PublicKey, existing.PublicKey)
		keep(&incoming.AppPublicCert, existing.AppPublicCert)
		keep(&incoming.AlipayPublicCert, existing.AlipayPublicCert)
		keep(&incoming.AlipayRootCert, existing.AlipayRootCert)
	}
	return s.SaveConfig(ctx, incoming)
}

// IsEnabled 是否启用支付宝支付，config.yaml 优先于 Setting 表
func (s *AlipayService) IsEnabled(ctx context.Context) bool {
	// config.yaml 中 AppID 非空时，以 enabled 字段为准
	if s.cfg.Alipay.AppID != "" {
		return s.cfg.Alipay.Enabled
	}
	// 回落到 Setting 表
	val, err := s.settingRepo.GetValue(ctx, SettingKeyAlipayEnabled)
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(val)) == "true"
}

// SetEnabled 启用/禁用支付宝支付
func (s *AlipayService) SetEnabled(ctx context.Context, enabled bool) error {
	v := "false"
	if enabled {
		v = "true"
	}
	return s.settingRepo.Set(ctx, SettingKeyAlipayEnabled, v)
}

// CreateOrder 创建支付宝当面付订单，返回二维码链接。
// packageID > 0 时从套餐中查询金额；packageID == 0 时使用 cnyAmount（自定义金额，单位：元，1 CNY = 10 U）。
func (s *AlipayService) CreateOrder(ctx context.Context, userID int64, packageID int, cnyAmount float64) (*AlipayOrder, error) {
	if !s.IsEnabled(ctx) {
		return nil, ErrAlipayNotEnabled
	}

	notifyURL, valid := s.NotifyURL(ctx)
	if !valid {
		return nil, ErrAlipayNotConfigured
	}

	var pkgName string
	var pkgCnyAmount float64
	var pkgUsdAmount float64

	if packageID > 0 {
		// 从套餐查询
		pkgs, err := s.packages(ctx)
		if err != nil {
			return nil, err
		}
		var pkg *AlipayPackage
		for i := range pkgs {
			if pkgs[i].ID == packageID {
				pkg = &pkgs[i]
				break
			}
		}
		if pkg == nil {
			return nil, ErrAlipayInvalidPackage
		}
		pkgName = strings.TrimSpace(pkg.Name)
		if pkgName == "" {
			pkgName = fmt.Sprintf("充值 ¥%.2f", pkg.CnyAmount)
		}
		pkgCnyAmount = pkg.CnyAmount
		pkgUsdAmount = pkg.UsdAmount
	} else {
		// 自定义金额，1 CNY = RMBToU U
		if cnyAmount < 1 || cnyAmount > 50000 {
			return nil, infraerrors.BadRequest("ALIPAY_INVALID_AMOUNT", "amount must be between ¥1 and ¥50000")
		}
		pkgName = fmt.Sprintf("自定义充值 ¥%.2f", cnyAmount)
		pkgCnyAmount = cnyAmount
		pkgUsdAmount = cnyAmount * RMBToU
	}

	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	orderNo, err := generateAlipayOrderNo()
	if err != nil {
		return nil, fmt.Errorf("generate order no: %w", err)
	}

	client, err := s.getOrBuildClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("build alipay client: %w", err)
	}

	cnyFee := int(math.Round(pkgCnyAmount * 100))
	totalAmount := fmt.Sprintf("%d.%02d", cnyFee/100, cnyFee%100)

	bm := make(gopay.BodyMap)
	bm.Set("subject", pkgName).
		Set("out_trade_no", orderNo).
		Set("total_amount", totalAmount).
		Set("notify_url", notifyURL)
	if cfg.SellerID != "" {
		bm.Set("seller_id", cfg.SellerID)
	}

	resp, err := client.TradePrecreate(ctx, bm)
	if err != nil {
		return nil, fmt.Errorf("alipay precreate: %w", err)
	}
	if resp.Response == nil || resp.Response.QrCode == "" {
		code, msg := "", ""
		if resp.Response != nil {
			code = resp.Response.Code
			msg = resp.Response.Msg
		}
		return nil, fmt.Errorf("alipay precreate: empty qr_code, code=%s msg=%s", code, msg)
	}

	qrCode := resp.Response.QrCode

	order := &AlipayOrder{
		OrderNo:   orderNo,
		UserID:    userID,
		PackageID: packageID,
		CnyFee:    cnyFee,
		UsdAmount: pkgUsdAmount,
		Status:    "pending",
		QRCode:    &qrCode,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	if err := s.orderRepo.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}

	log.Printf("alipay: order created: order_no=%s user_id=%d package_id=%d cny_fee=%d usd_amount=%.4f",
		order.OrderNo, order.UserID, order.PackageID, order.CnyFee, order.UsdAmount)

	return order, nil
}

// GetOrderStatus 查询订单状态（前端轮询用）
func (s *AlipayService) GetOrderStatus(ctx context.Context, userID int64, orderNo string) (*AlipayOrder, error) {
	order, err := s.orderRepo.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order.UserID != userID {
		return nil, ErrAlipayOrderNotFound
	}
	if order.Status == "pending" && time.Now().After(order.ExpiresAt) {
		order.Status = "expired"
	}
	return order, nil
}

// HandleNotify 处理支付宝异步通知（已由 handler 层完成验签）
func (s *AlipayService) HandleNotify(ctx context.Context, notifyMap map[string]any) (bool, error) {
	tradeStatus, _ := notifyMap["trade_status"].(string)
	outTradeNo, _ := notifyMap["out_trade_no"].(string)
	alipayTradeNo, _ := notifyMap["trade_no"].(string)

	if tradeStatus != "TRADE_SUCCESS" && tradeStatus != "TRADE_FINISHED" {
		log.Printf("alipay notify: ignored: out_trade_no=%s trade_status=%s", outTradeNo, tradeStatus)
		return false, nil
	}

	if outTradeNo == "" || alipayTradeNo == "" {
		log.Printf("alipay notify: missing required fields: out_trade_no=%q trade_no=%q trade_status=%s", outTradeNo, alipayTradeNo, tradeStatus)
		return false, nil
	}

	notifyJSON, err := json.Marshal(notifyMap)
	if err != nil {
		log.Printf("alipay notify: marshal notify data failed (audit loss): %v", err)
		notifyJSON = []byte("{}")
	}

	tx, err := s.db.Tx(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	defer func() { _ = tx.Rollback() }()

	updated, err := s.orderRepo.MarkPaid(txCtx, outTradeNo, alipayTradeNo, string(notifyJSON))
	if err != nil {
		return false, fmt.Errorf("mark paid: %w", err)
	}
	if !updated {
		return false, nil
	}

	order, err := s.orderRepo.GetByOrderNo(txCtx, outTradeNo)
	if err != nil {
		return false, fmt.Errorf("get order: %w", err)
	}

	if err := s.userService.UpdateBalance(txCtx, order.UserID, order.UsdAmount); err != nil {
		return false, fmt.Errorf("update balance: user_id=%d amount=%f err=%w", order.UserID, order.UsdAmount, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit tx: %w", err)
	}

	log.Printf("alipay: order paid: order_no=%s alipay_trade_no=%s user_id=%d cny_fee=%d usd_amount=%.4f",
		order.OrderNo, alipayTradeNo, order.UserID, order.CnyFee, order.UsdAmount)

	return true, nil
}

// ListOrdersByUser 获取用户充值记录
func (s *AlipayService) ListOrdersByUser(ctx context.Context, userID int64, params pagination.PaginationParams) ([]AlipayOrder, *pagination.PaginationResult, error) {
	return s.orderRepo.ListByUser(ctx, userID, params)
}

// ListOrders 管理员查询订单列表
func (s *AlipayService) ListOrders(ctx context.Context, params pagination.PaginationParams, status string) ([]AlipayOrder, *pagination.PaginationResult, error) {
	return s.orderRepo.List(ctx, params, status)
}

// GetPackages 返回当前配置的充值套餐列表
func (s *AlipayService) GetPackages(ctx context.Context) ([]AlipayPackage, error) {
	return s.packages(ctx)
}

// SavePackages 覆盖保存充值套餐配置
func (s *AlipayService) SavePackages(ctx context.Context, pkgs []AlipayPackage) error {
	if pkgs == nil {
		pkgs = []AlipayPackage{}
	}
	for i := range pkgs {
		pkgs[i].Name = strings.TrimSpace(pkgs[i].Name)
		if pkgs[i].Name == "" {
			return infraerrors.BadRequest("ALIPAY_INVALID_PACKAGE", "package name is required")
		}
		if pkgs[i].CnyAmount <= 0 || pkgs[i].UsdAmount <= 0 {
			return infraerrors.BadRequest("ALIPAY_INVALID_PACKAGE", "package amount must be positive")
		}
	}
	b, err := json.Marshal(pkgs)
	if err != nil {
		return fmt.Errorf("marshal packages: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyAlipayPackages, string(b))
}

// packages 读取 Setting 表中的支付宝套餐配置
func (s *AlipayService) packages(ctx context.Context) ([]AlipayPackage, error) {
	val, err := s.settingRepo.GetValue(ctx, SettingKeyAlipayPackages)
	if err != nil || val == "" {
		return []AlipayPackage{}, nil
	}
	var pkgs []AlipayPackage
	if err := json.Unmarshal([]byte(val), &pkgs); err != nil {
		return nil, fmt.Errorf("unmarshal packages: %w", err)
	}
	return pkgs, nil
}

// ---- 工具函数 ----

func generateAlipayOrderNo() (string, error) {
	const chars = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		b[i] = chars[n.Int64()]
	}
	return fmt.Sprintf("AP%d%s", time.Now().UnixMilli(), string(b)), nil
}

func buildAlipayClient(cfg *AlipayConfig) (*alipay.Client, error) {
	// 处理字面 \n，剥去 PEM headers，得到裸 base64
	privateKey := stripPEMHeaders(strings.ReplaceAll(cfg.PrivateKey, `\n`, "\n"))

	// gopay 要求 PKCS1 格式的裸 base64；若用户提供的是 PKCS8，自动转换
	privateKey, err := ensurePKCS1(privateKey)
	if err != nil {
		return nil, fmt.Errorf("convert private key to PKCS1: %w", err)
	}

	client, err := alipay.NewClient(cfg.AppID, privateKey, cfg.IsProd)
	if err != nil {
		return nil, fmt.Errorf("new alipay client: %w", err)
	}

	switch cfg.modeOrDefault() {
	case AlipayModeCert:
		appCert := normalizePEM(cfg.AppPublicCert)
		alipayCert := normalizePEM(cfg.AlipayPublicCert)
		rootCert := normalizePEM(cfg.AlipayRootCert)
		if appCert == "" || alipayCert == "" || rootCert == "" {
			return nil, ErrAlipayNotConfigured
		}
		if err := client.SetCertSnByContent([]byte(appCert), []byte(rootCert), []byte(alipayCert)); err != nil {
			return nil, fmt.Errorf("set alipay cert sn: %w", err)
		}
		// 证书模式下用支付宝公钥证书做异步通知自动验签
		client.AutoVerifySign([]byte(alipayCert))
	default:
		if cfg.PublicKey != "" {
			pubKeyPEM := strings.ReplaceAll(cfg.PublicKey, `\n`, "\n")
			if !strings.Contains(pubKeyPEM, "-----") {
				pubKeyPEM = "-----BEGIN PUBLIC KEY-----\n" + pubKeyPEM + "\n-----END PUBLIC KEY-----\n"
			}
			client.AutoVerifySign([]byte(pubKeyPEM))
		}
	}

	return client, nil
}

// normalizePEM 将字面 \n 转为真实换行，并去掉首尾空白；空值返回 ""
func normalizePEM(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, `\n`, "\n"))
}

// ensurePKCS1 若输入是 PKCS8 裸 base64，转为 PKCS1 裸 base64；PKCS1 直接返回
func ensurePKCS1(bareBase64 string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(bareBase64)
	if err != nil {
		return bareBase64, nil // 解码失败，透传让 gopay 自己报错
	}
	// 尝试按 PKCS8 解析
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		// 不是 PKCS8，原值返回（可能已是 PKCS1）
		return bareBase64, nil
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA (got %T)", key)
	}
	pkcs1DER := x509.MarshalPKCS1PrivateKey(rsaKey)
	return base64.StdEncoding.EncodeToString(pkcs1DER), nil
}

// stripPEMHeaders 去掉 PEM 的 BEGIN/END 行和空行，返回裸 base64 字符串
func stripPEMHeaders(pem string) string {
	var lines []string
	for _, line := range strings.Split(pem, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-----") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "")
}

// VerifyNotifySign 验证支付宝异步通知签名，自动适配公钥/证书模式
func (s *AlipayService) VerifyNotifySign(ctx context.Context, notifyMap gopay.BodyMap) (bool, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	switch cfg.modeOrDefault() {
	case AlipayModeCert:
		alipayCert := normalizePEM(cfg.AlipayPublicCert)
		if alipayCert == "" {
			return false, ErrAlipayNotConfigured
		}
		return alipay.VerifySignWithCert([]byte(alipayCert), notifyMap)
	default:
		pubKey := stripPEMHeaders(strings.ReplaceAll(cfg.PublicKey, `\n`, "\n"))
		if pubKey == "" {
			return false, ErrAlipayNotConfigured
		}
		return alipay.VerifySign(pubKey, notifyMap)
	}
}
