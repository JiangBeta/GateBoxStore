package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiangBeta/GateBoxStore/plugins/mosdns/backend/mosdns"
)

// newTestServer 构造仅含本插件的测试服务器（未安装 mosdns 二进制 → 进程态降级）。
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	m := mosdns.NewManager(dir)
	if err := m.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	h := &api{m: m, sup: &supervisor{dir: dir}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /settings", h.getSettings)
	mux.HandleFunc("PUT /settings", h.putSettings)
	mux.HandleFunc("GET /config", h.getConfig)
	mux.HandleFunc("PUT /config", h.putConfig)
	mux.HandleFunc("GET /hosts", h.getHosts)
	mux.HandleFunc("PUT /hosts", h.putHosts)
	mux.HandleFunc("GET /rules", h.listRules)
	mux.HandleFunc("GET /rules/{name}", h.getRule)
	mux.HandleFunc("PUT /rules/{name}", h.putRule)
	mux.HandleFunc("GET /geodata", h.listGeodata)
	mux.HandleFunc("POST /flush", h.flush)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func req(t *testing.T, method, url, raw string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if raw != "" {
		rdr = bytes.NewReader([]byte(raw))
	}
	r, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestStatusAndSettings(t *testing.T) {
	ts := newTestServer(t)

	code, body := req(t, http.MethodGet, ts.URL+"/status", "")
	if code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", code, body)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("status 非 JSON: %v", err)
	}
	if st["configExists"] != false {
		t.Errorf("初始 configExists 应为 false: %v", st["configExists"])
	}
	if st["listen"] != mosdns.DefaultListen {
		t.Errorf("默认 listen 错误: %v", st["listen"])
	}

	code, body = req(t, http.MethodPut, ts.URL+"/settings",
		`{"listen":":5353","logLevel":"warn","localDns":["1.1.1.1"],"remoteDns":["tls://8.8.8.8"],"cache":true,"cacheSize":4096}`)
	if code != http.StatusOK {
		t.Fatalf("putSettings code=%d body=%s", code, body)
	}

	code, body = req(t, http.MethodGet, ts.URL+"/config", "")
	if code != http.StatusOK || !strings.Contains(body, `"configured":true`) {
		t.Fatalf("getConfig code=%d body=%s", code, body)
	}
	if !strings.Contains(body, ":5353") {
		t.Errorf("生成配置应含监听端口: %s", body)
	}
}

func TestSettingsValidation(t *testing.T) {
	ts := newTestServer(t)
	cases := []string{
		`{"listen":"noport","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"]}`,
		`{"listen":":53","localDns":[],"remoteDns":["8.8.8.8"]}`,
		`{"listen":":53","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"],"cache":true,"cacheSize":0}`,
	}
	for _, c := range cases {
		if code, _ := req(t, http.MethodPut, ts.URL+"/settings", c); code != http.StatusBadRequest {
			t.Errorf("非法设置应 400, body=%s code=%d", c, code)
		}
	}
}

func TestHostsEndpoints(t *testing.T) {
	ts := newTestServer(t)
	req(t, http.MethodPut, ts.URL+"/settings",
		`{"listen":":5335","logLevel":"info","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"],"cache":false,"cacheSize":0}`)

	code, body := req(t, http.MethodPut, ts.URL+"/hosts",
		`{"hosts":[{"domain":"router.lan","ips":["192.168.1.1"]},{"domain":"nas.home","ips":["192.168.1.2","2001:db8::1"]}]}`)
	if code != http.StatusOK {
		t.Fatalf("putHosts code=%d body=%s", code, body)
	}
	code, body = req(t, http.MethodGet, ts.URL+"/hosts", "")
	if code != http.StatusOK || !strings.Contains(body, "router.lan") {
		t.Fatalf("getHosts code=%d body=%s", code, body)
	}
	if code, _ := req(t, http.MethodPut, ts.URL+"/hosts",
		`{"hosts":[{"domain":"bad.lan","ips":["not-ip"]}]}`); code != http.StatusBadRequest {
		t.Errorf("非法 IP 应 400, code=%d", code)
	}
}

func TestConfigValidation(t *testing.T) {
	ts := newTestServer(t)
	if code, _ := req(t, http.MethodPut, ts.URL+"/config", `{"content":"plugins: []"}`); code != http.StatusBadRequest {
		t.Error("空 plugins 应 400")
	}
	valid := `{"content":"log:\n  level: info\nplugins:\n  - tag: hosts\n    type: hosts\n    args: {}\n"}`
	if code, body := req(t, http.MethodPut, ts.URL+"/config", valid); code != http.StatusOK {
		t.Errorf("合法配置应 200, code=%d body=%s", code, body)
	}
}

func TestFlushWithoutCache(t *testing.T) {
	ts := newTestServer(t)
	req(t, http.MethodPut, ts.URL+"/settings",
		`{"listen":":5335","logLevel":"info","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"],"cache":false,"cacheSize":0}`)
	if code, _ := req(t, http.MethodPost, ts.URL+"/flush", ""); code != http.StatusBadGateway {
		t.Errorf("无缓存插件时刷新应 502, code=%d", code)
	}
}

func TestRulesEndpoints(t *testing.T) {
	ts := newTestServer(t)
	code, body := req(t, http.MethodGet, ts.URL+"/rules", "")
	if code != http.StatusOK || !strings.Contains(body, "whitelist") || !strings.Contains(body, "cloudflare-cidr") {
		t.Fatalf("规则清单异常: code=%d body=%s", code, body)
	}
	code, body = req(t, http.MethodPut, ts.URL+"/rules/blocklist", `{"content":"ads.example.com"}`)
	if code != http.StatusOK {
		t.Fatalf("写规则失败: code=%d body=%s", code, body)
	}
	code, body = req(t, http.MethodGet, ts.URL+"/rules/blocklist", "")
	if code != http.StatusOK || !strings.Contains(body, "ads.example.com") {
		t.Fatalf("读规则失败: code=%d body=%s", code, body)
	}
	if code, _ := req(t, http.MethodGet, ts.URL+"/rules/nope", ""); code != http.StatusNotFound {
		t.Errorf("未知规则应 404, code=%d", code)
	}
}

func TestGeodataList(t *testing.T) {
	ts := newTestServer(t)
	code, body := req(t, http.MethodGet, ts.URL+"/geodata", "")
	if code != http.StatusOK || !strings.Contains(body, "geosite_cn.txt") || !strings.Contains(body, "geoip_cn.txt") {
		t.Fatalf("数据库清单异常: code=%d body=%s", code, body)
	}
}
