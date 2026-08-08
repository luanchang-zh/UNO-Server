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
	"github.com/luanchang-zh/UNO-Server/internal/idgen"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/server"
	"github.com/luanchang-zh/UNO-Server/internal/store"
	mysqlstore "github.com/luanchang-zh/UNO-Server/internal/store/mysql"
	redisstore "github.com/luanchang-zh/UNO-Server/internal/store/redis"
)

// main 只负责呈现进程级错误并设置退出码，资源释放统一留在 run 中完成。
func main() {
	logger := logx.New(slog.LevelInfo)
	rootCtx := context.Background()
	if err := run(rootCtx, logger); err != nil {
		logger.WithContext(rootCtx).Error("服务退出", "event", "process_exit", "result", "error", "error", err)
		os.Exit(1)
	}
	logger.WithContext(rootCtx).Info("服务已退出", "event", "process_exit", "result", "ok")
}

// run 加载配置，装配 MySQL、Redis 与业务服务，并在退出前释放全部进程资源。
func run(rootCtx context.Context, logger *logx.Logger) error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	idGenerator, err := idgen.New(cfg.NodeID)
	if err != nil {
		return fmt.Errorf("create id generator: %w", err)
	}
	playerRepository, matchRepository, closeMySQL, err := openMySQLRepositories(rootCtx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeMySQL()
	sessionRepository, snapshotRepository, closeRedis, err := openRedisRepositories(rootCtx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeRedis()

	authService := auth.NewService(auth.Options{
		TokenTTL:           cfg.TokenTTL,
		MaxNicknameLen:     cfg.MaxNicknameLen,
		IDGenerator:        idGenerator,
		PlayerRepository:   playerRepository,
		PersistenceTimeout: cfg.MySQLOperationTimeout,
		SessionRepository:  sessionRepository,
		SessionTimeout:     cfg.RedisOperationTimeout,
	})
	httpServer := server.New(cfg, authService, logger, server.Dependencies{
		IDGenerator:            idGenerator,
		MatchRepository:        matchRepository,
		RoomSnapshotRepository: snapshotRepository,
	})
	restoreCtx, cancelRestore := context.WithTimeout(rootCtx, cfg.RedisOperationTimeout)
	err = httpServer.RestoreRooms(restoreCtx)
	cancelRestore()
	if err != nil {
		return err
	}

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
			return fmt.Errorf("serve http: %w", err)
		}
	case sig := <-signalCh:
		logger.WithContext(rootCtx).Info("收到退出信号", "event", "shutdown_signal", "signal", sig.String())
		ctx, cancel := context.WithTimeout(rootCtx, cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		// 等待 Listen 协程退出（正常关闭时为 nil）。
		if err := <-errCh; err != nil {
			return fmt.Errorf("serve after shutdown: %w", err)
		}
	}
	return nil
}

// openMySQLRepositories 按配置启用 MySQL，并返回避免 nil 指针装入接口的仓储端口。
func openMySQLRepositories(
	rootCtx context.Context,
	cfg config.Config,
	logger *logx.Logger,
) (store.PlayerRepository, store.MatchRepository, func(), error) {
	if cfg.MySQLDSN == "" {
		logger.WithContext(rootCtx).Warn("MySQL 持久化未启用", "event", "mysql_setup", "result", "disabled")
		return nil, nil, func() {}, nil
	}
	openCtx, cancelOpen := context.WithTimeout(rootCtx, cfg.MySQLOperationTimeout)
	repository, err := mysqlstore.Open(openCtx, mysqlstore.Options{
		DSN:             cfg.MySQLDSN,
		MaxOpenConns:    cfg.MySQLMaxOpenConns,
		MaxIdleConns:    cfg.MySQLMaxIdleConns,
		ConnMaxLifetime: cfg.MySQLConnMaxLifetime,
		ConnMaxIdleTime: cfg.MySQLConnMaxIdleTime,
	})
	cancelOpen()
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("open mysql repository: %w", err)
	}
	if cfg.MySQLAutoMigrate {
		migrateCtx, cancelMigrate := context.WithTimeout(rootCtx, cfg.MySQLOperationTimeout)
		err = repository.Migrate(migrateCtx)
		cancelMigrate()
		if err != nil {
			_ = repository.Close()
			return nil, nil, func() {}, fmt.Errorf("migrate mysql: %w", err)
		}
	}
	logger.WithContext(rootCtx).Info("MySQL 持久化已启用", "event", "mysql_setup", "result", "enabled")
	return repository, repository, func() { _ = repository.Close() }, nil
}

// openRedisRepositories 按配置启用 Redis，并让 token 与房间快照复用同一个连接池。
func openRedisRepositories(
	rootCtx context.Context,
	cfg config.Config,
	logger *logx.Logger,
) (store.SessionRepository, store.RoomSnapshotRepository, func(), error) {
	if cfg.RedisAddr == "" {
		logger.WithContext(rootCtx).Warn("Redis 持久化未启用", "event", "redis_setup", "result", "disabled")
		return nil, nil, func() {}, nil
	}
	openCtx, cancel := context.WithTimeout(rootCtx, cfg.RedisOperationTimeout)
	repository, err := redisstore.Open(openCtx, redisstore.Options{
		Addr:         cfg.RedisAddr,
		Username:     cfg.RedisUsername,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		PoolSize:     cfg.RedisPoolSize,
		MinIdleConns: cfg.RedisMinIdleConns,
		DialTimeout:  cfg.RedisDialTimeout,
		ReadTimeout:  cfg.RedisReadTimeout,
		WriteTimeout: cfg.RedisWriteTimeout,
	})
	cancel()
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("open redis repository: %w", err)
	}
	logger.WithContext(rootCtx).Info(
		"Redis token 与房间快照持久化已启用",
		"event", "redis_setup",
		"result", "enabled",
	)
	return repository, repository, func() { _ = repository.Close() }, nil
}
