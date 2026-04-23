package provisioner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Blu-7/go_zabbix/internal/config"
	"github.com/Blu-7/go_zabbix/internal/discovery"
	"github.com/Blu-7/go_zabbix/internal/zabbix"
)

type Service struct {
	cfg       config.Config
	logger    *slog.Logger
	discovery *discovery.Client
	zabbix    *zabbix.Client
}

func NewService(cfg config.Config, logger *slog.Logger, discoveryClient *discovery.Client, zabbixClient *zabbix.Client) *Service {
	return &Service{
		cfg:       cfg,
		logger:    logger.With("component", "provisioner"),
		discovery: discoveryClient,
		zabbix:    zabbixClient,
	}
}

func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("starting provisioner", "interval", s.cfg.DiscoveryInterval.String(), "source", s.cfg.TenantAPIURL)

	s.runCycle(ctx)

	ticker := time.NewTicker(s.cfg.DiscoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopped")
			return nil
		case <-ticker.C:
			s.runCycle(ctx)
		}
	}
}

func (s *Service) runCycle(ctx context.Context) {
	if err := s.runCycleErr(ctx); err != nil {
		s.logger.Error("sync cycle failed", "error", err)
	}
}

func (s *Service) runCycleErr(ctx context.Context) error {
	s.logger.Info("sync cycle start")

	if err := s.zabbix.Connect(ctx); err != nil {
		return err
	}

	tenants, err := s.discovery.FetchActiveTenants(ctx)
	if err != nil {
		return err
	}

	groupID, err := s.zabbix.EnsureHostGroup(ctx)
	if err != nil {
		return err
	}

	if err := s.zabbix.MigrateLegacyHosts(ctx, groupID); err != nil {
		return err
	}

	activeDomains := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		if err := s.zabbix.SyncTenant(ctx, tenant, groupID); err != nil {
			return err
		}
		activeDomains[tenant.Domain] = struct{}{}
	}
	if err := s.zabbix.DisableRemovedTenants(ctx, activeDomains, groupID); err != nil {
		return err
	}

	s.logger.Info("sync cycle done", "tenants", len(activeDomains))
	return nil
}
