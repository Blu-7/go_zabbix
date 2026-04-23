package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Blu-7/go_zabbix/internal/config"
	"github.com/Blu-7/go_zabbix/internal/discovery"
	"github.com/Blu-7/go_zabbix/internal/provisioner"
	"github.com/Blu-7/go_zabbix/internal/zabbix"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load(".env")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	discoveryClient := discovery.NewClient(cfg, logger)
	zabbixClient := zabbix.NewClient(cfg, logger)
	service := provisioner.NewService(cfg, logger, discoveryClient, zabbixClient)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := service.Run(ctx); err != nil {
		logger.Error("provisioner stopped with error", "error", err)
		os.Exit(1)
	}
}
