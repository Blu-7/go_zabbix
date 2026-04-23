package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TenantAPIURL     string
	TenantAPIKey     string
	TenantAPITimeout time.Duration

	HealthcheckProbeTimeout       time.Duration
	HealthcheckRelPath            string
	HealthcheckAPIKey             string
	HealthcheckProbeUserAgent     string
	HealthcheckProbeAccept        string
	HealthcheckFallbackHTTPStatus int
	DiscoveryInterval             time.Duration
	LogLevel                      slog.Level
	ZabbixAPIURL                  string
	ZabbixAPIUser                 string
	ZabbixAPIPassword             string
	ZabbixHostGroup               string
	ZabbixWebScenarioNameTemplate string
	ZabbixWebStepNameTemplate     string
	ZabbixWebCheckDelay           string
	ZabbixWebCheckRetries         int
	ZabbixWebCheckStatusCodes     string
	ZabbixWebCheckTimeout         string
	ZabbixWebCheckFollowRedirects int
	ZabbixWebSlowSeconds          int
	ZabbixTriggerDownRspCodes     string
}

func LoadFromEnv() (Config, error) {
	cfg := Config{
		TenantAPIURL:     getEnv("TENANT_API_URL", "https://quyettam.cloud/get-tenant.php"),
		TenantAPIKey:     strings.TrimSpace(os.Getenv("TENANT_API_KEY")),
		TenantAPITimeout: time.Duration(getEnvInt("TENANT_API_TIMEOUT", 30)) * time.Second,

		HealthcheckProbeTimeout:       time.Duration(getEnvInt("HEALTHCHECK_PROBE_TIMEOUT", 10)) * time.Second,
		HealthcheckRelPath:            normalizeRelPath(getEnv("HEALTHCHECK_REL_PATH", "/webhooks/healthcheck.php")),
		HealthcheckAPIKey:             getEnv("HEALTHCHECK_API_KEY", ""),
		HealthcheckProbeUserAgent:     getEnv("HEALTHCHECK_PROBE_USER_AGENT", "tenant-discovery-go/1.0"),
		HealthcheckProbeAccept:        getEnv("HEALTHCHECK_PROBE_ACCEPT", "*/*"),
		HealthcheckFallbackHTTPStatus: getEnvInt("HEALTHCHECK_FALLBACK_HTTP_STATUS", 404),
		DiscoveryInterval:             time.Duration(getEnvInt("DISCOVERY_INTERVAL", 300)) * time.Second,
		LogLevel:                      parseLogLevel(getEnv("LOG_LEVEL", "INFO")),

		ZabbixAPIURL:                  getEnv("ZABBIX_API_URL", "http://zabbix-web:8080/api_jsonrpc.php"),
		ZabbixAPIUser:                 getEnv("ZABBIX_API_USER", "Admin"),
		ZabbixAPIPassword:             getEnv("ZABBIX_API_PASSWORD", "zabbix"),
		ZabbixHostGroup:               getEnv("ZABBIX_HOST_GROUP", "Tenant Web Services"),
		ZabbixWebScenarioNameTemplate: getEnv("ZABBIX_WEB_SCENARIO_NAME_TEMPLATE", "HTTP Check - {domain}"),
		ZabbixWebStepNameTemplate:     getEnv("ZABBIX_WEB_STEP_NAME_TEMPLATE", "GET {domain}"),
		ZabbixWebCheckDelay:           getEnv("ZABBIX_WEB_CHECK_DELAY", "120s"),
		ZabbixWebCheckRetries:         getEnvInt("ZABBIX_WEB_CHECK_RETRIES", 1),
		ZabbixWebCheckStatusCodes:     getEnv("ZABBIX_WEB_CHECK_STATUS_CODES", "200"),
		ZabbixWebCheckTimeout:         getEnv("ZABBIX_WEB_CHECK_TIMEOUT", "10s"),
		ZabbixWebCheckFollowRedirects: getEnvInt("ZABBIX_WEB_CHECK_FOLLOW_REDIRECTS", 1),
		ZabbixWebSlowSeconds:          getEnvInt("ZABBIX_WEB_SLOW_SECONDS", 10),
		ZabbixTriggerDownRspCodes:     getEnv("ZABBIX_TRIGGER_DOWN_RSP_CODES", "500,502,503,504"),
	}

	if cfg.HealthcheckAPIKey == "" {
		cfg.HealthcheckAPIKey = cfg.TenantAPIKey
	}
	if cfg.TenantAPIKey == "" {
		return Config{}, fmt.Errorf("TENANT_API_KEY must not be empty")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func normalizeRelPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

func parseLogLevel(v string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
