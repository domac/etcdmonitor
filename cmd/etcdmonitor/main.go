package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	etcdmonitor "etcdmonitor"
	"etcdmonitor/internal/api"
	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/collector"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/kvmanager"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/prefs"
	"etcdmonitor/internal/storage"

	"github.com/gin-gonic/gin"
)

// Version 版本号，构建时通过 -ldflags 注入
var Version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	showVersion := flag.Bool("v", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("etcdmonitor version %s\n", Version)
		os.Exit(0)
	}

	fmt.Println("==============================================")
	fmt.Printf("  etcdmonitor v%s - etcd Monitoring Dashboard\n", Version)
	fmt.Println("==============================================")

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化 zap 日志
	if err := logger.Init(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Infof("Log directory: %s", cfg.Log.Dir)
	logger.Infof("Log level: %s", cfg.Log.Level)
	logger.Infof("etcd endpoints: %v", cfg.EtcdEndpoints())
	logger.Infof("Collect interval: %ds", cfg.Collector.Interval)
	logger.Infof("Data retention: %d days", cfg.Storage.RetentionDays)

	// 初始化全局健康端点管理器（探测所有地址，全部不可用则退出）
	healthMgr, err := health.New(cfg)
	if err != nil {
		logger.Fatalf("Init health manager: %v", err)
	}
	go healthMgr.StartBackgroundCheck()
	defer healthMgr.Close()
	logger.Infof("Healthy endpoints: %v", healthMgr.HealthyEndpoints())

	// 初始化存储
	store, err := storage.New(cfg)
	if err != nil {
		logger.Fatalf("Init storage: %v", err)
	}
	defer store.Close()
	logger.Infof("Storage initialized: %s", cfg.Storage.DBPath)

	// 初始化采集器（先于认证检测启动，确保立即采集数据）
	coll := collector.New(cfg, store, healthMgr)
	go coll.Start()
	defer coll.Stop()

	// 检测 etcd 认证状态（仅影响 Dashboard 访问控制）
	dashboardAuthRequired := auth.DetectAuthRequired(cfg, healthMgr)
	if dashboardAuthRequired {
		logger.Infof("Dashboard auth mode: enabled (login required)")
	} else {
		logger.Infof("Dashboard auth mode: disabled (open access)")
	}

	// 初始化会话存储
	sessionStore := auth.NewMemorySessionStore()
	defer sessionStore.Stop()

	// 初始化用户偏好存储（文件存储，目录自动创建）
	prefsDir := filepath.Join(filepath.Dir(cfg.Storage.DBPath), "user-prefs")
	prefsStore := prefs.NewFileStore(prefsDir)
	logger.Infof("User preferences directory: %s", prefsDir)

	// 初始化 Gin Engine
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard

	router := gin.New()

	// 全局中间件：Recovery → Logger → SecurityHeaders
	router.Use(api.GinRecovery())
	router.Use(api.GinZapLogger())
	router.Use(api.SecurityHeadersMiddleware(cfg))

	// 初始化 API 并注册路由
	a := api.New(cfg, store, coll, healthMgr, dashboardAuthRequired, sessionStore, prefsStore, Version)
	protected := a.SetupRoutes(router)

	// 初始化 KV 管理模块
	kvHandler, err := kvmanager.NewKVHandler(cfg, logger.L(), healthMgr)
	if err != nil {
		logger.Warnf("KV manager init failed (KV management will be unavailable): %v", err)
	} else {
		kvHandler.RegisterRoutes(protected)
		defer kvHandler.Close()
		logger.Info("KV manager initialized")
	}

	// 静态文件服务（嵌入的 web 目录）
	webContent, err := fs.Sub(etcdmonitor.WebFS, "web")
	if err != nil {
		logger.Fatalf("Setup web fs: %v", err)
	}
	router.NoRoute(gin.WrapH(http.FileServer(http.FS(webContent))))

	// 启动 HTTP/HTTPS 服务
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           router,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	go func() {
		if cfg.Server.TLSEnable {
			logger.Infof("Dashboard listening on %s (HTTPS)", cfg.Server.Listen)
			fmt.Printf("\n  >>> Open https://localhost%s in your browser <<<\n\n", cfg.Server.Listen)
			if err := server.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey); err != http.ErrServerClosed {
				logger.Fatalf("HTTPS server: %v", err)
			}
		} else {
			logger.Infof("Dashboard listening on %s (HTTP)", cfg.Server.Listen)
			fmt.Printf("\n  >>> Open http://localhost%s in your browser <<<\n\n", cfg.Server.Listen)
			if err := server.ListenAndServe(); err != http.ErrServerClosed {
				logger.Fatalf("HTTP server: %v", err)
			}
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down...")

	// 给正在处理的请求最多 10 秒完成
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warnf("Server shutdown error: %v", err)
	}
	logger.Info("Bye!")
}
