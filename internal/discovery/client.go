package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Blu-7/go_zabbix/internal/config"
)

type Tenant struct {
	TenantCode     string
	TenantName     string
	Domain         string
	LicenseStatus  string
	ExpiredDate    string
	HealthEndpoint string
}

type Client struct {
	cfg    config.Config
	logger *slog.Logger
	http   *http.Client
}

type endpointAPIResponse struct {
	Success int             `json:"success"`
	Message string          `json:"message"`
	Tenants json.RawMessage `json:"tenants"`
}

type tenantRow struct {
	ID             any    `json:"id"`
	Name           string `json:"name"`
	TenantName     string `json:"tenant_name"`
	PrimarySiteURL string `json:"primary_site_url"`
	LicenseStatus  string `json:"license_status"`
	ExpiredDate    string `json:"expired_date"`
}

func NewClient(cfg config.Config, logger *slog.Logger) *Client {
	return &Client{
		cfg:    cfg,
		logger: logger.With("component", "discovery"),
		http:   &http.Client{Timeout: cfg.TenantAPITimeout},
	}
}

func (c *Client) FetchActiveTenants(ctx context.Context) ([]Tenant, error) {
	payload, _ := json.Marshal(map[string]string{"password": c.cfg.TenantAPIKey})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TenantAPIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tenant api status: %d", resp.StatusCode)
	}

	var out endpointAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Success != 1 {
		return nil, fmt.Errorf("tenant api success=%d message=%s", out.Success, out.Message)
	}

	rows, err := parseTenantRows(out.Tenants)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	tenants := make([]Tenant, 0, len(rows))
	for _, row := range rows {
		domain, base := domainAndBase(row.PrimarySiteURL)
		healthURL := strings.TrimRight(base, "/") + c.cfg.HealthcheckRelPath
		status, probeErr := c.probeHealthRoute(ctx, healthURL)

		healthEndpoint := healthURL
		if probeErr != nil {
			c.logger.Warn("health route probe failed; fallback to base", "domain", domain, "error", probeErr)
			healthEndpoint = base
		} else if status == c.cfg.HealthcheckFallbackHTTPStatus {
			healthEndpoint = base
		}

		key := domain + "|" + healthEndpoint
		if _, ok := seen[key]; ok {
			c.logger.Warn("duplicate tenant after normalization; skipping", "domain", domain, "health_endpoint", healthEndpoint)
			continue
		}
		seen[key] = struct{}{}

		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = strings.TrimSpace(row.TenantName)
		}
		if name == "" {
			name = domain
		}

		tenants = append(tenants, Tenant{
			TenantCode:     fmt.Sprintf("%v", row.ID),
			TenantName:     name,
			Domain:         domain,
			LicenseStatus:  row.LicenseStatus,
			ExpiredDate:    row.ExpiredDate,
			HealthEndpoint: healthEndpoint,
		})
	}

	c.logger.Info("fetched tenants", "count", len(tenants))
	return tenants, nil
}

func parseTenantRows(raw json.RawMessage) ([]tenantRow, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []tenantRow{}, nil
	}
	var asArray []tenantRow
	if err := json.Unmarshal(raw, &asArray); err == nil {
		return asArray, nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		return nil, fmt.Errorf("unsupported tenants payload format")
	}
	if strings.TrimSpace(asString) == "" {
		return []tenantRow{}, nil
	}
	if err := json.Unmarshal([]byte(asString), &asArray); err != nil {
		return nil, err
	}
	return asArray, nil
}

func domainAndBase(primarySiteURL string) (string, string) {
	s := strings.TrimSpace(primarySiteURL)
	if s == "" {
		return "", ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		domain := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		return domain, strings.TrimRight(s, "/")
	}
	return u.Host, strings.TrimRight(u.String(), "/")
}

func (c *Client) probeHealthRoute(ctx context.Context, healthURL string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.cfg.HealthcheckProbeUserAgent)
	req.Header.Set("Accept", c.cfg.HealthcheckProbeAccept)

	probeHTTP := &http.Client{Timeout: c.cfg.HealthcheckProbeTimeout}
	resp, err := probeHTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
