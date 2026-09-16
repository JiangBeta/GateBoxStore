// Command ddns-go-sidecar 是 GateBox 的独立进程插件后端：DDNS 上报（ddns-go）。
//
// 消费型插件（ADR-036 I2）：经投影 API + reconcile 自收敛——
//   - 拉取 domains / credentials 投影（带 plugin token）；
//   - 渲染 ddns-go 配置 .ddns_go_config.yaml 并原子写盘；
//   - 代管 ddns-go 本体进程（ADR-037 决策 (a)）。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	dataDir := envOr("GATEBOX_DATA_DIR", ".")
	port := envOr("GATEBOX_PLUGIN_PORT", "8099")
	token := os.Getenv("GATEBOX_PLUGIN_TOKEN")
	coreURL := strings.TrimRight(os.Getenv("GATEBOX_CORE_URL"), "/")

	dir := filepath.Join(dataDir, "tools", "ddnsgo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}
	s := &server{
		coreURL: coreURL,
		token:   token,
		dir:     dir,
		gopath:  filepath.Join(dir, ".ddns_go_config.yaml"),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
	s.sup = &supervisor{dir: dir}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.reconcileLoop(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("POST /sync", s.sync)
	mux.HandleFunc("POST /start", s.start)
	mux.HandleFunc("POST /stop", s.stop)
	mux.HandleFunc("POST /restart", s.restart)

	addr := "127.0.0.1:" + port
	log.Printf("ddns-go 插件后端启动: %s, 运行目录 %s", addr, dir)
	// 收到信号时先停 ddns-go 本体再退出，避免孤儿进程。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("收到退出信号：停止 ddns-go 本体")
		_ = s.sup.stop()
		os.Exit(0)
	}()

	srv := &http.Server{Addr: addr, Handler: withToken(token, mux), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

type server struct {
	coreURL string
	token   string
	dir     string
	gopath  string
	client  *http.Client
	sup     *supervisor
	mu      sync.Mutex
	rev     string
	lastMsg string
}

func withToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("X-Plugin-Token") != token {
			http.Error(w, "plugin token 无效", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- 投影消费与收敛 ---

func (s *server) reconcileLoop(ctx context.Context) {
	// 首次立即收敛。
	s.reconcileOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// 长轮询：revision 变化即返回并收敛；超时则周期性重试。
		changed := s.waitForChange(ctx, s.revision(), 30)
		if changed {
			s.reconcileOnce(ctx)
		}
		time.Sleep(2 * time.Second)
	}
}

func (s *server) revision() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev
}

// waitForChange 长轮询 domains 投影，返回是否发生变更。
func (s *server) waitForChange(ctx context.Context, since string, waitSec int) bool {
	if s.coreURL == "" {
		time.Sleep(time.Duration(waitSec) * time.Second)
		return true
	}
	url := fmt.Sprintf("%s/api/v1/extensions/me/projection/domains?since=%s&wait=%d", s.coreURL, since, waitSec)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-Plugin-Token", s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		time.Sleep(5 * time.Second)
		return false
	}
	defer resp.Body.Close()
	var p domainsProjection
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return false
	}
	next := strconv.FormatUint(p.Revision, 10)
	s.mu.Lock()
	changed := next != s.rev
	s.rev = next
	s.mu.Unlock()
	return changed
}

func (s *server) reconcileOnce(ctx context.Context) {
	doms, err := s.fetchDomains(ctx)
	if err != nil {
		s.setMsg("拉取 domains 投影失败: " + err.Error())
		return
	}
	creds, err := s.fetchCredentials(ctx)
	if err != nil {
		s.setMsg("拉取 credentials 投影失败: " + err.Error())
		return
	}
	s.mu.Lock()
	s.rev = strconv.FormatUint(doms.Revision, 10)
	s.mu.Unlock()

	entries := buildEntries(doms.Domains, creds)
	if err := s.writeConfig(entries); err != nil {
		s.setMsg("写配置失败: " + err.Error())
		return
	}
	s.setMsg(fmt.Sprintf("已收敛：%d 个凭证 / %d 条域名", len(entries), countDomains(entries)))
}

func (s *server) fetchDomains(ctx context.Context) (domainsProjection, error) {
	var out domainsProjection
	err := s.getJSON(ctx, "/api/v1/extensions/me/projection/domains", &out)
	return out, err
}

func (s *server) fetchCredentials(ctx context.Context) ([]credential, error) {
	var out struct {
		Credentials []credential `json:"credentials"`
	}
	err := s.getJSON(ctx, "/api/v1/extensions/me/projection/credentials", &out)
	return out.Credentials, err
}

func (s *server) getJSON(ctx context.Context, path string, out any) error {
	if s.coreURL == "" {
		return fmt.Errorf("未配置 GATEBOX_CORE_URL")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.coreURL+path, nil)
	req.Header.Set("X-Plugin-Token", s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *server) setMsg(m string) {
	s.mu.Lock()
	s.lastMsg = m
	s.mu.Unlock()
	log.Println(m)
}

// writeConfig 原子写盘（临时文件 + rename），ddns-go 周期性重读，无需重启。
func (s *server) writeConfig(entries []Entry) error {
	data, err := buildConfig(entries)
	if err != nil {
		return err
	}
	tmp := s.gopath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, s.gopath)
}

// --- 投影数据结构 ---

type projectedDomain struct {
	Host         string `json:"host"`
	Protocol     string `json:"protocol"`
	RootDomain   string `json:"rootDomain"`
	Type         string `json:"type"`
	CredentialID string `json:"credentialId"`
}

type domainsProjection struct {
	Revision uint64            `json:"revision"`
	Domains  []projectedDomain `json:"domains"`
}

type ddnsMapping struct {
	Provider    string `json:"provider"`
	IDField     string `json:"idField"`
	SecretField string `json:"secretField"`
}

type credential struct {
	ID       string            `json:"id"`
	Provider string            `json:"provider"`
	Fields   map[string]string `json:"fields"`
	DDNS     *ddnsMapping      `json:"ddns"`
}

// --- 条目聚合与配置渲染（移植自核心 adapter/ddns）---

// Entry 一个 DNS 凭证 + 一组待上报域名。
type Entry struct {
	Provider string // GateBox 供应商名
	DDNSName string // ddns-go 供应商名
	ID       string
	Secret   string
	Domains  []string
}

func buildEntries(doms []projectedDomain, creds []credential) []Entry {
	credBy := make(map[string]credential, len(creds))
	for _, c := range creds {
		credBy[c.ID] = c
	}
	type acc struct {
		e   Entry
		set map[string]bool
	}
	byCred := map[string]*acc{}
	for _, d := range doms {
		if d.CredentialID == "" || d.Host == "" {
			continue
		}
		c, ok := credBy[d.CredentialID]
		if !ok || c.DDNS == nil {
			continue
		}
		id := c.Fields[c.DDNS.IDField]
		secret := c.Fields[c.DDNS.SecretField]
		if secret == "" || (c.DDNS.IDField != "" && id == "") {
			continue
		}
		a, ok := byCred[d.CredentialID]
		if !ok {
			a = &acc{e: Entry{Provider: c.Provider, DDNSName: c.DDNS.Provider, ID: id, Secret: secret}, set: map[string]bool{}}
			byCred[d.CredentialID] = a
		}
		a.set[d.Host] = true
	}
	out := make([]Entry, 0, len(byCred))
	for _, a := range byCred {
		for h := range a.set {
			a.e.Domains = append(a.e.Domains, h)
		}
		sort.Strings(a.e.Domains)
		out = append(out, a.e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Secret < out[j].Secret
	})
	return out
}

func countDomains(entries []Entry) int {
	n := 0
	for _, e := range entries {
		n += len(e.Domains)
	}
	return n
}

func buildConfig(entries []Entry) ([]byte, error) {
	cfg := config{}
	for _, e := range entries {
		name := e.DDNSName
		if name == "" {
			name = e.Provider
		}
		cfg.DnsConf = append(cfg.DnsConf, dnsConf{
			Name: e.Provider,
			Ipv4: ipv4Config{Enable: true, GetType: "url", URL: "https://api.ipify.org", Domains: e.Domains},
			Ipv6: ipv6Config{Enable: false},
			DNS:  dnsConfig{Name: name, ID: e.ID, Secret: e.Secret},
		})
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

type config struct {
	DnsConf []dnsConf `yaml:"dnsconf"`
}

type dnsConf struct {
	Name string     `yaml:"name"`
	Ipv4 ipv4Config `yaml:"ipv4"`
	Ipv6 ipv6Config `yaml:"ipv6"`
	DNS  dnsConfig  `yaml:"dns"`
}

type dnsConfig struct {
	Name     string `yaml:"name"`
	ID       string `yaml:"id"`
	Secret   string `yaml:"secret"`
	ExtParam string `yaml:"extparam,omitempty"`
}

type ipv4Config struct {
	Enable  bool     `yaml:"enable"`
	GetType string   `yaml:"gettype"`
	URL     string   `yaml:"url,omitempty"`
	Domains []string `yaml:"domains,omitempty"`
}

type ipv6Config struct {
	Enable bool `yaml:"enable"`
}

// --- 进程托管（ddns-go 本体）---

type supervisor struct{ dir string }

func (s *supervisor) bin() string  { return filepath.Join(s.dir, "ddns-go") }
func (s *supervisor) pidF() string { return filepath.Join(s.dir, "ddns-go.pid") }
func (s *supervisor) logF() string { return filepath.Join(s.dir, "ddns-go.log") }

func (s *supervisor) installed() bool { _, err := os.Stat(s.bin()); return err == nil }
func (s *supervisor) pid() int {
	b, err := os.ReadFile(s.pidF())
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return p
}
func (s *supervisor) running() bool { p := s.pid(); return p > 0 && syscall.Kill(p, 0) == nil }

func (s *supervisor) start(configPath string) error {
	if s.running() {
		return nil
	}
	if !s.installed() {
		return fmt.Errorf("ddns-go 未安装: %s", s.bin())
	}
	_ = os.Remove(s.pidF())
	lf, _ := os.OpenFile(s.logF(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	cmd := exec.Command(s.bin(), "-c", configPath)
	cmd.Dir = s.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if lf != nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = os.WriteFile(s.pidF(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	time.Sleep(300 * time.Millisecond)
	if !s.running() {
		return fmt.Errorf("ddns-go 启动后立即退出，日志: %s", s.logF())
	}
	return nil
}

func (s *supervisor) stop() error {
	p := s.pid()
	if p <= 0 {
		return nil
	}
	_ = syscall.Kill(p, syscall.SIGTERM)
	for i := 0; i < 30; i++ {
		if syscall.Kill(p, 0) != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if syscall.Kill(p, 0) == nil {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
	_ = os.Remove(s.pidF())
	return nil
}

func (s *supervisor) version() string {
	if !s.installed() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.bin(), "-v").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// --- HTTP 处理 ---

func (s *server) status(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	msg := s.lastMsg
	s.mu.Unlock()
	state := "unknown"
	if s.sup.running() {
		state = "running"
	} else if s.sup.installed() {
		state = "stopped"
	}
	_, cfgErr := os.Stat(s.gopath)
	writeJSON(w, http.StatusOK, map[string]any{
		"installed":  s.sup.installed(),
		"state":      state,
		"healthy":    s.sup.running(),
		"version":    s.sup.version(),
		"configPath": s.gopath,
		"configured": cfgErr == nil,
		"message":    msg,
	})
}

func (s *server) sync(w http.ResponseWriter, r *http.Request) {
	s.reconcileOnce(r.Context())
	s.status(w, r)
}

func (s *server) start(w http.ResponseWriter, r *http.Request) {
	if err := s.sup.start(s.gopath); err != nil {
		writeErrCode(w, http.StatusBadRequest, "START_FAILED", err.Error())
		return
	}
	s.status(w, r)
}

func (s *server) stop(w http.ResponseWriter, r *http.Request) {
	if err := s.sup.stop(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "STOP_FAILED", err.Error())
		return
	}
	s.status(w, r)
}

func (s *server) restart(w http.ResponseWriter, r *http.Request) {
	_ = s.sup.stop()
	if err := s.sup.start(s.gopath); err != nil {
		writeErrCode(w, http.StatusBadRequest, "RESTART_FAILED", err.Error())
		return
	}
	s.status(w, r)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
