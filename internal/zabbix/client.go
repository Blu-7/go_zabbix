package zabbix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Blu-7/go_zabbix/internal/config"
	"github.com/Blu-7/go_zabbix/internal/discovery"
)

type Client struct {
	cfg    config.Config
	logger *slog.Logger
	http   *http.Client
	auth   string
	id     int64
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
	ID      int64           `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

type tag struct {
	Tag   string `json:"tag"`
	Value string `json:"value"`
}

type header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type host struct {
	HostID string `json:"hostid"`
	Host   string `json:"host"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Tags   []tag  `json:"tags"`
	Macros []struct {
		HostMacroID string `json:"hostmacroid"`
		Macro       string `json:"macro"`
		Value       string `json:"value"`
	} `json:"macros"`
}

type scenario struct {
	HTTPTestID string `json:"httptestid"`
	Name       string `json:"name"`
	Steps      []struct {
		HTTPStepID string   `json:"httpstepid"`
		URL        string   `json:"url"`
		Name       string   `json:"name"`
		Posts      string   `json:"posts"`
		Headers    []header `json:"headers"`
	} `json:"steps"`
}

type trigger struct {
	TriggerID   string `json:"triggerid"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
	OpData      string `json:"opdata"`
	Flags       string `json:"flags"`
}

func NewClient(cfg config.Config, logger *slog.Logger) *Client {
	return &Client{
		cfg:    cfg,
		logger: logger.With("component", "zabbix"),
		http:   &http.Client{},
		id:     1,
	}
}

func (c *Client) Connect(ctx context.Context) error {
	if c.auth != "" {
		return nil
	}
	var token string
	if err := c.call(ctx, "user.login", map[string]any{
		"username": c.cfg.ZabbixAPIUser,
		"password": c.cfg.ZabbixAPIPassword,
	}, false, &token); err != nil {
		return err
	}
	c.auth = token
	return nil
}

func (c *Client) EnsureHostGroup(ctx context.Context) (string, error) {
	var groups []struct {
		GroupID string `json:"groupid"`
	}
	if err := c.call(ctx, "hostgroup.get", map[string]any{
		"filter": map[string]any{"name": []string{c.cfg.ZabbixHostGroup}},
	}, true, &groups); err != nil {
		return "", err
	}
	if len(groups) > 0 {
		return groups[0].GroupID, nil
	}
	var created struct {
		GroupIDs []string `json:"groupids"`
	}
	if err := c.call(ctx, "hostgroup.create", map[string]any{"name": c.cfg.ZabbixHostGroup}, true, &created); err != nil {
		return "", err
	}
	return created.GroupIDs[0], nil
}

func (c *Client) MigrateLegacyHosts(ctx context.Context, groupID string) error {
	var hosts []host
	if err := c.call(ctx, "host.get", map[string]any{
		"groupids":     groupID,
		"output":       []string{"hostid", "host"},
		"selectMacros": []string{"hostmacroid", "macro", "value"},
	}, true, &hosts); err != nil {
		return err
	}
	for _, h := range hosts {
		if !strings.HasPrefix(h.Host, "tenant-") {
			continue
		}
		domain := ""
		for _, m := range h.Macros {
			if m.Macro == "{$TENANT.DOMAIN}" {
				domain = m.Value
				break
			}
		}
		if domain == "" {
			continue
		}
		var conflict []host
		if err := c.call(ctx, "host.get", map[string]any{
			"filter": map[string]any{"host": domain},
			"output": []string{"hostid"},
		}, true, &conflict); err != nil {
			return err
		}
		if len(conflict) > 0 {
			continue
		}
		if err := c.call(ctx, "host.update", map[string]any{
			"hostid": h.HostID,
			"host":   domain,
		}, true, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) SyncTenant(ctx context.Context, tenant discovery.Tenant, groupID string) error {
	hostName := tenant.Domain
	scenarioName := strings.ReplaceAll(c.cfg.ZabbixWebScenarioNameTemplate, "{domain}", tenant.Domain)
	stepName := strings.ReplaceAll(c.cfg.ZabbixWebStepNameTemplate, "{domain}", tenant.Domain)

	hostID, err := c.ensureHost(ctx, tenant, groupID)
	if err != nil {
		return err
	}
	if err := c.ensureHostTags(ctx, hostID, tenant); err != nil {
		return err
	}
	if err := c.ensureHostMacro(ctx, hostID, tenant.Domain); err != nil {
		return err
	}
	if err := c.ensureWebScenario(ctx, hostID, tenant, scenarioName, stepName); err != nil {
		return err
	}
	if err := c.ensureTriggers(ctx, hostID, hostName, tenant, scenarioName, stepName); err != nil {
		return err
	}
	return nil
}

func (c *Client) DisableRemovedTenants(ctx context.Context, activeDomains map[string]struct{}, groupID string) error {
	var hosts []host
	if err := c.call(ctx, "host.get", map[string]any{
		"groupids": groupID,
		"output":   []string{"hostid", "host", "status"},
	}, true, &hosts); err != nil {
		return err
	}
	for _, h := range hosts {
		if _, ok := activeDomains[h.Host]; ok {
			continue
		}
		if h.Status == "0" {
			if err := c.call(ctx, "host.update", map[string]any{
				"hostid": h.HostID,
				"status": 1,
			}, true, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) ensureHost(ctx context.Context, tenant discovery.Tenant, groupID string) (string, error) {
	var existing []host
	if err := c.call(ctx, "host.get", map[string]any{
		"filter": map[string]any{"host": tenant.Domain},
		"output": []string{"hostid", "status"},
	}, true, &existing); err != nil {
		return "", err
	}
	if len(existing) > 0 {
		if existing[0].Status == "1" {
			if err := c.call(ctx, "host.update", map[string]any{
				"hostid": existing[0].HostID,
				"status": 0,
			}, true, nil); err != nil {
				return "", err
			}
		}
		return existing[0].HostID, nil
	}

	visibleName := tenant.TenantName
	if visibleName == "" {
		visibleName = tenant.Domain
	}
	var created struct {
		HostIDs []string `json:"hostids"`
	}
	if err := c.call(ctx, "host.create", map[string]any{
		"host":   tenant.Domain,
		"name":   visibleName,
		"groups": []map[string]string{{"groupid": groupID}},
		"tags": []map[string]string{
			{"tag": "tenantId", "value": tenant.TenantCode},
			{"tag": "licenseStatus", "value": tenant.LicenseStatus},
			{"tag": "expiredDate", "value": tenant.ExpiredDate},
		},
	}, true, &created); err != nil {
		return "", err
	}
	return created.HostIDs[0], nil
}

func (c *Client) ensureHostTags(ctx context.Context, hostID string, tenant discovery.Tenant) error {
	var hosts []host
	if err := c.call(ctx, "host.get", map[string]any{
		"hostids":    hostID,
		"output":     []string{"hostid"},
		"selectTags": []string{"tag", "value"},
	}, true, &hosts); err != nil {
		return err
	}
	current := map[string]string{}
	if len(hosts) > 0 {
		for _, t := range hosts[0].Tags {
			current[t.Tag] = t.Value
		}
	}
	current["tenantId"] = tenant.TenantCode
	current["licenseStatus"] = tenant.LicenseStatus
	current["expiredDate"] = tenant.ExpiredDate

	tags := make([]map[string]string, 0, len(current))
	for k, v := range current {
		tags = append(tags, map[string]string{"tag": k, "value": v})
	}
	if err := c.call(ctx, "host.update", map[string]any{
		"hostid": hostID,
		"tags":   tags,
	}, true, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) ensureHostMacro(ctx context.Context, hostID, domain string) error {
	var hosts []host
	if err := c.call(ctx, "host.get", map[string]any{
		"hostids":      hostID,
		"output":       []string{"hostid"},
		"selectMacros": []string{"hostmacroid", "macro", "value"},
	}, true, &hosts); err != nil {
		return err
	}
	for _, m := range hosts[0].Macros {
		if m.Macro == "{$TENANT.DOMAIN}" {
			if m.Value == domain {
				return nil
			}
			return c.call(ctx, "usermacro.update", map[string]any{
				"hostmacroid": m.HostMacroID,
				"value":       domain,
			}, true, nil)
		}
	}
	return c.call(ctx, "usermacro.create", map[string]any{
		"hostid": hostID,
		"macro":  "{$TENANT.DOMAIN}",
		"value":  domain,
	}, true, nil)
}

func (c *Client) ensureWebScenario(ctx context.Context, hostID string, tenant discovery.Tenant, scenarioName, stepName string) error {
	posts, headers := c.stepExtras(tenant.HealthEndpoint)
	var scenarios []scenario
	if err := c.call(ctx, "httptest.get", map[string]any{
		"hostids":     hostID,
		"output":      []string{"httptestid", "name"},
		"selectSteps": []string{"httpstepid", "url", "name", "posts", "headers"},
	}, true, &scenarios); err != nil {
		return err
	}

	if len(scenarios) > 0 {
		chosen := scenarios[0]
		for _, s := range scenarios {
			if s.Name == scenarioName {
				chosen = s
				break
			}
		}
		extraIDs := make([]string, 0)
		for _, s := range scenarios {
			if s.HTTPTestID != chosen.HTTPTestID {
				extraIDs = append(extraIDs, s.HTTPTestID)
			}
		}
		if len(extraIDs) > 0 {
			params := make([]any, 0, len(extraIDs))
			for _, id := range extraIDs {
				params = append(params, id)
			}
			if err := c.call(ctx, "httptest.delete", params, true, nil); err != nil {
				return err
			}
		}
		if len(chosen.Steps) == 0 {
			return fmt.Errorf("web scenario %s has no steps", chosen.Name)
		}

		step := chosen.Steps[0]
		stepPatch := map[string]any{"httpstepid": step.HTTPStepID}
		if step.URL != tenant.HealthEndpoint {
			stepPatch["url"] = tenant.HealthEndpoint
		}
		if step.Name != stepName {
			stepPatch["name"] = stepName
		}
		if step.Posts != posts {
			stepPatch["posts"] = posts
		}
		if !headersEqual(step.Headers, headers) {
			stepPatch["headers"] = headers
		}

		payload := map[string]any{"httptestid": chosen.HTTPTestID}
		if chosen.Name != scenarioName {
			payload["name"] = scenarioName
		}
		if len(stepPatch) > 1 {
			payload["steps"] = []map[string]any{stepPatch}
		}
		if len(payload) > 1 {
			if err := c.call(ctx, "httptest.update", payload, true, nil); err != nil {
				return err
			}
		}
		return nil
	}

	return c.call(ctx, "httptest.create", map[string]any{
		"name":    scenarioName,
		"hostid":  hostID,
		"delay":   c.cfg.ZabbixWebCheckDelay,
		"retries": c.cfg.ZabbixWebCheckRetries,
		"status":  0,
		"steps": []map[string]any{{
			"name":             stepName,
			"url":              tenant.HealthEndpoint,
			"status_codes":     c.cfg.ZabbixWebCheckStatusCodes,
			"no":               1,
			"timeout":          c.cfg.ZabbixWebCheckTimeout,
			"follow_redirects": c.cfg.ZabbixWebCheckFollowRedirects,
			"posts":            posts,
			"headers":          headers,
		}},
	}, true, nil)
}

func (c *Client) ensureTriggers(ctx context.Context, hostID, hostName string, tenant discovery.Tenant, scenarioName, stepName string) error {
	downDesc := fmt.Sprintf("[%s] SITE DOWN - %s", tenant.TenantName, tenant.Domain)
	slowDesc := fmt.Sprintf("[%s] Slow response - %s", tenant.TenantName, tenant.Domain)

	codes := splitCodes(c.cfg.ZabbixTriggerDownRspCodes)
	if len(codes) == 0 {
		codes = []string{"500", "502", "503", "504"}
	}
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("last(/%s/web.test.rspcode[%s,%s])=%s", hostName, scenarioName, stepName, code))
	}
	downExpr := strings.Join(parts, " or ")
	downOpData := "Status code: {ITEM.VALUE}"
	slowExpr := fmt.Sprintf("last(/%s/web.test.time[%s,%s,resp])>%d", hostName, scenarioName, stepName, c.cfg.ZabbixWebSlowSeconds)

	if err := c.upsertTrigger(ctx, hostID, downDesc, downExpr, downOpData, 4, []map[string]string{
		{"tag": "scope", "value": "availability"},
		{"tag": "tenant", "value": tenant.TenantCode},
	}); err != nil {
		return err
	}
	if err := c.upsertTrigger(ctx, hostID, slowDesc, slowExpr, "", 2, []map[string]string{
		{"tag": "scope", "value": "performance"},
		{"tag": "tenant", "value": tenant.TenantCode},
	}); err != nil {
		return err
	}
	return c.deleteUnmanagedTriggers(ctx, hostID, map[string]struct{}{downDesc: {}, slowDesc: {}})
}

func (c *Client) upsertTrigger(ctx context.Context, hostID, desc, expr, opData string, priority int, tags []map[string]string) error {
	var triggers []trigger
	if err := c.call(ctx, "trigger.get", map[string]any{
		"hostids": hostID,
		"filter":  map[string]any{"description": desc},
		"output":  []string{"triggerid", "expression", "opdata"},
	}, true, &triggers); err != nil {
		return err
	}
	if len(triggers) > 0 {
		patch := map[string]any{"triggerid": triggers[0].TriggerID}
		if triggers[0].Expression != expr {
			patch["expression"] = expr
		}
		if opData != "" && triggers[0].OpData != opData {
			patch["opdata"] = opData
		}
		if len(patch) > 1 {
			return c.call(ctx, "trigger.update", patch, true, nil)
		}
		return nil
	}
	payload := map[string]any{
		"description": desc,
		"expression":  expr,
		"priority":    priority,
		"tags":        tags,
	}
	if opData != "" {
		payload["opdata"] = opData
	}
	return c.call(ctx, "trigger.create", payload, true, nil)
}

func (c *Client) deleteUnmanagedTriggers(ctx context.Context, hostID string, managed map[string]struct{}) error {
	var triggers []trigger
	if err := c.call(ctx, "trigger.get", map[string]any{
		"hostids": hostID,
		"output":  []string{"triggerid", "description", "flags"},
	}, true, &triggers); err != nil {
		return err
	}
	for _, t := range triggers {
		if _, ok := managed[t.Description]; ok {
			continue
		}
		flags, _ := strconv.Atoi(t.Flags)
		if flags == 4 {
			continue
		}
		if err := c.call(ctx, "trigger.delete", []string{t.TriggerID}, true, nil); err != nil {
			c.logger.Warn("cannot delete unmanaged trigger", "trigger", t.Description, "id", t.TriggerID, "error", err)
		}
	}
	return nil
}

func (c *Client) stepExtras(healthEndpoint string) (string, []header) {
	rel := strings.TrimRight(c.cfg.HealthcheckRelPath, "/")
	if rel != "" && strings.HasSuffix(strings.TrimRight(healthEndpoint, "/"), rel) {
		body, _ := json.Marshal(map[string]string{"password": c.cfg.HealthcheckAPIKey})
		return string(body), []header{{Name: "Content-Type", Value: "application/json"}}
	}
	return "", []header{}
}

func splitCodes(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func headersEqual(a, b []header) bool {
	normalize := func(in []header) []string {
		out := make([]string, 0, len(in))
		for _, h := range in {
			out = append(out, h.Name+"="+h.Value)
		}
		sort.Strings(out)
		return out
	}
	na := normalize(a)
	nb := normalize(b)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		if na[i] != nb[i] {
			return false
		}
	}
	return true
}

func (c *Client) call(ctx context.Context, method string, params any, withAuth bool, out any) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      c.id,
	}
	c.id++
	if withAuth {
		req["auth"] = c.auth
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.ZabbixAPIURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json-rpc")
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("zabbix api status: %d", httpResp.StatusCode)
	}

	var rpc rpcResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&rpc); err != nil {
		return err
	}
	if rpc.Error != nil {
		return fmt.Errorf("zabbix %s failed: %s (%s)", method, rpc.Error.Message, rpc.Error.Data)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rpc.Result, out)
}
