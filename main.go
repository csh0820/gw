package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/csh0820/gw/config"
	"github.com/csh0820/gw/internal/discovery"
	"github.com/csh0820/gw/internal/gateway"
	"github.com/csh0820/gw/internal/pool"
	"github.com/csh0820/gw/pkg/etcd"
)

var (
	logger *zap.Logger
)

func init() {
	// 初始化日志
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		log.Fatal("Failed to create logger:", err)
	}

	defer logger.Sync()
}

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 初始化ETCD客户端
	etcdClient, err := etcd.NewClientFromConfig(logger)
	if err != nil {
		logger.Fatal("Failed to create ETCD client", zap.Error(err))
	}
	defer etcdClient.Close()

	// 创建服务注册中心
	registry := discovery.NewEtcdRegistry(etcdClient, logger)

	// 创建HTTP连接池
	httpPool := pool.NewHTTPPool(&pool.Config{
		MaxIdleConns:        cfg.HTTPPool.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.HTTPPool.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.HTTPPool.MaxConnsPerHost,
		IdleConnTimeout:     time.Duration(cfg.HTTPPool.IdleConnTimeout) * time.Second,
	})

	// 创建网关处理器
	gatewayHandler := gateway.NewHandler(registry, httpPool, cfg, logger)

	// 启动服务发现监听
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gatewayHandler.StartServiceDiscovery(ctx)

	// 设置Gin模式
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建Gin引擎
	router := gin.New()

	// 使用中间件
	router.Use(gateway.RecoveryMiddleware(logger))
	router.Use(gateway.LoggingMiddleware(logger))
	router.Use(gateway.CorsMiddleware())
	router.Use(gateway.RateLimitMiddleware(cfg.RateLimit))

	// 注册路由
	gatewayHandler.RegisterRoutes(router)

	// 健康检查
	// router.GET("/health", health.Health)

	// 管理端点
	// router.GET("/admin/services", gatewayHandler.ListServices)
	// router.POST("/admin/services/refresh", gatewayHandler.RefreshServices)

	// 创建HTTP服务器
	server := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	// 优雅关闭
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	logger.Info("Gateway started", zap.String("address", cfg.Server.Address))

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down gateway...")

	// 设置关闭超时
	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()

	if err := server.Shutdown(ctxShutdown); err != nil {
		logger.Error("Server shutdown failed", zap.Error(err))
	}

	logger.Info("Gateway exited")
}
