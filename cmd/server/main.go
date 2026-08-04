// Package main 是 UNO 游戏服务进程入口。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/server"
)

// main 加载配置、启动 HTTP 服务，并在收到退出信号后优雅关闭。
func main() {
	logger := logx.New(slog.LevelInfo)
	// 进程级日志无业务 trace，使用空 context 即可。
	rootCtx := context.Background()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		logger.WithContext(rootCtx).Error("配置无效", "error", err)
		os.Exit(1)
	}

	authService := auth.NewService(auth.Options{
		TokenTTL:       cfg.TokenTTL,
		MaxNicknameLen: cfg.MaxNicknameLen,
	})
	httpServer := server.New(cfg, authService, logger)

	// Start 在 ErrServerClosed 时返回 nil，其它监听错误经 channel 上报。
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Start()
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			logger.WithContext(rootCtx).Error("服务异常退出", "error", err)
			os.Exit(1)
		}
	case sig := <-signalCh:
		logger.WithContext(rootCtx).Info("收到退出信号", "signal", sig.String())
		ctx, cancel := context.WithTimeout(rootCtx, cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.WithContext(ctx).Error("优雅关闭失败", "error", fmt.Errorf("shutdown: %w", err))
			os.Exit(1)
		}
		// 等待 Listen 协程退出（正常关闭时为 nil）。
		if err := <-errCh; err != nil {
			logger.WithContext(rootCtx).Error("关闭后服务返回错误", "error", err)
			os.Exit(1)
		}
	}

	logger.WithContext(rootCtx).Info("服务已退出")
}
