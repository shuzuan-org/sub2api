package main

//go:generate go run github.com/google/wire/cmd/wire

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/metrics"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/Wei-Shaw/sub2api/internal/web"

	"github.com/cloudflare/tableflip"
	"github.com/gin-gonic/gin"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

//go:embed VERSION
var embeddedVersion string

// Build-time variables (can be set by ldflags)
var (
	Version   = ""
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "source" // "source" for manual builds, "release" for CI builds (set by ldflags)
)

func init() {
	// 如果 Version 已通过 ldflags 注入（例如 -X main.Version=...），则不要覆盖。
	if strings.TrimSpace(Version) != "" {
		return
	}

	// 默认从 embedded VERSION 文件读取版本号（编译期打包进二进制）。
	Version = strings.TrimSpace(embeddedVersion)
	if Version == "" {
		Version = "0.0.0-dev"
	}
}

// initLogger configures the default slog handler based on gin.Mode().
// In non-release mode, Debug level logs are enabled.
func main() {
	logger.InitBootstrap()
	defer logger.Sync()

	// Parse command line flags
	setupMode := flag.Bool("setup", false, "Run setup wizard in CLI mode")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		log.Printf("Sub2API %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	// CLI setup mode
	if *setupMode {
		if err := setup.RunCLI(); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
		return
	}

	// Check if setup is needed
	if setup.NeedsSetup() {
		// Check if auto-setup is enabled (for Docker deployment)
		if setup.AutoSetupEnabled() {
			log.Println("Auto setup mode enabled...")
			if err := setup.AutoSetupFromEnv(); err != nil {
				log.Fatalf("Auto setup failed: %v", err)
			}
			// Continue to main server after auto-setup
		} else {
			log.Println("First run detected, starting setup wizard...")
			runSetupServer()
			return
		}
	}

	// Normal server mode
	runMainServer()
}

func runSetupServer() {
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(config.CORSConfig{}))
	r.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true, Policy: config.DefaultCSPPolicy}, nil))

	// Register setup routes
	setup.RegisterRoutes(r)

	// Serve embedded frontend if available
	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}

	// Get server address from config.yaml or environment variables (SERVER_HOST, SERVER_PORT)
	// This allows users to run setup on a different address if needed
	addr := config.GetServerAddress()
	log.Printf("Setup wizard available at http://%s", addr)
	log.Println("Complete the setup wizard to configure Sub2API")

	server := &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(r, &http2.Server{}),
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Failed to start setup server: %v", err)
	}
}

func runMainServer() {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	if cfg.RunMode == config.RunModeSimple {
		log.Println("⚠️  WARNING: Running in SIMPLE mode - billing and quota checks are DISABLED")
	}

	buildInfo := handler.BuildInfo{
		Version:   Version,
		BuildType: BuildType,
	}

	app, err := initializeApplication(buildInfo)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.Cleanup()

	// 首次部署时把 SettingService 的默认开关写入数据库（含 registration_enabled=true）。
	// 已存在 registration_enabled 时此调用为 no-op，因此对升级场景安全。
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := app.SettingService.InitializeDefaultSettings(initCtx); err != nil {
		log.Printf("Failed to initialize default settings: %v", err)
	}
	initCancel()

	// 零停机热升级：通过 tableflip 在 SIGHUP 时 fork+exec（磁盘上已替换的）新二进制，并把监听 socket fd
	// 交给新进程，于是部署期间不丢任何连接。旧进程排空在途请求（含长流式响应）后再退出。
	upg, err := tableflip.New(tableflip.Options{})
	if err != nil {
		log.Fatalf("tableflip init failed: %v", err)
	}
	defer upg.Stop()

	// stopping 区分真正停机（SIGINT/SIGTERM，需告知 systemd STOPPING=1）与升级交接（SIGHUP，不可发 STOPPING，
	// 否则 systemd 进入 deactivating 杀掉新进程）。
	var stopping atomic.Bool

	// SIGHUP -> 零停机升级
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP)
		for range sigCh {
			log.Println("upgrade requested (SIGHUP)")
			if err := upg.Upgrade(); err != nil {
				log.Printf("upgrade failed: %v", err)
			}
		}
	}()

	// SIGINT/SIGTERM -> 优雅停机（解除 upg.Exit() 阻塞）
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received %v, stopping...", sig)
		stopping.Store(true)
		upg.Stop()
	}()

	// 通过 tableflip 创建/继承监听 socket（升级时新进程从 fd 继承，端口无空窗）
	ln, err := upg.Listen("tcp", app.Server.Addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", app.Server.Addr, err)
	}

	go func() {
		if err := app.Server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	if err := upg.Ready(); err != nil {
		log.Fatalf("tableflip ready failed: %v", err)
	}
	// 告知 systemd 本进程（可能是 exec 出来的）已就绪并成为 main process。
	sdNotify("READY=1\nMAINPID=" + strconv.Itoa(os.Getpid()))
	if upg.HasParent() {
		metrics.DeployUpgradeTotal.Inc()
	}
	log.Printf("Server started on %s (pid=%d, upgraded=%t)", app.Server.Addr, os.Getpid(), upg.HasParent())

	// 阻塞直到被新进程接管（SIGHUP）或收到停机信号。
	<-upg.Exit()

	log.Printf("draining in-flight requests (stopping=%t)...", stopping.Load())
	if stopping.Load() {
		sdNotify("STOPPING=1")
	}

	// 排空在途请求。drainTimeout 远大于原来的 5s，以覆盖长流式/agent 请求，发布时不再被硬杀。
	drainStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout())
	defer cancel()
	if err := app.Server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error (drain timed out or forced): %v", err)
	}
	metrics.DrainDurationSeconds.Observe(time.Since(drainStart).Seconds())

	log.Println("Server exited")
}

// defaultDrainTimeout 是优雅停机/升级交接时排空在途请求的默认上限。
// 取 2h 以覆盖长流式/agent 请求（与兄弟项目 sglang-proxy/cc2codex 一致）。
// 注意：tableflip 在旧进程排空期间不拒绝新的升级，频繁 reload 会堆叠多个 draining 进程；
// 部署去抖/并发 drain 上限由部署脚本侧负责（deploy.sh）。
const defaultDrainTimeout = 2 * time.Hour

// drainTimeout 返回排空上限，可由环境变量 SUB2API_DRAIN_TIMEOUT 覆盖（如 "30m"、"1h"）。
func drainTimeout() time.Duration {
	if v := os.Getenv("SUB2API_DRAIN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		log.Printf("invalid SUB2API_DRAIN_TIMEOUT=%q, using default %s", v, defaultDrainTimeout)
	}
	return defaultDrainTimeout
}
