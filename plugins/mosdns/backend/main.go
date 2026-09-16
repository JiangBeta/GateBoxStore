// Command mosdns-sidecar 是 GateBox 的独立进程插件后端：内网 DNS（mosdns）管理。
//
// 由 GateBox 以 kind:process 插件托管（ADR-037/039）：
//   - 监听 127.0.0.1:$GATEBOX_PLUGIN_PORT；
//   - 内核把 /api/v1/plugins/mosdns/* 反代到本进程根路径；
//   - 请求须带 X-Plugin-Token（值 = $GATEBOX_PLUGIN_TOKEN）；
//   - 本进程自行管理 mosdns 本体二进制（start/stop/restart，ADR-037 决策 (a)）。
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/JiangBeta/GateBoxStore/plugins/mosdns/backend/mosdns"
)

func main() {
	dataDir := os.Getenv("GATEBOX_DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}
	port := os.Getenv("GATEBOX_PLUGIN_PORT")
	if port == "" {
		port = "8099"
	}
	token := os.Getenv("GATEBOX_PLUGIN_TOKEN")

	dir := filepath.Join(dataDir, "tools", "mosdns")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}
	m := mosdns.NewManager(dir)
	// 预建配置引用的数据/规则文件：mosdns 对缺失文件会拒绝启动。
	if err := m.EnsureLayout(); err != nil {
		log.Printf("初始化 mosdns 数据目录失败(继续): %v", err)
	}
	h := &api{m: m, sup: &supervisor{dir: dir}, token: token}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("POST /start", h.start)
	mux.HandleFunc("POST /stop", h.stop)
	mux.HandleFunc("POST /restart", h.restart)

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
	mux.HandleFunc("POST /geodata/update", h.updateGeodata)
	mux.HandleFunc("POST /adblock/update", h.updateAdblock)
	mux.HandleFunc("GET /logs", h.getLogs)
	mux.HandleFunc("GET /logs/stream", h.logsStream)
	mux.HandleFunc("DELETE /logs", h.clearLogs)
	mux.HandleFunc("POST /flush", h.flush)

	addr := "127.0.0.1:" + port
	log.Printf("mosdns 插件后端启动: %s, 运行目录 %s", addr, dir)

	// 收到信号（GateBox 停止本 sidecar）时，先停 mosdns 本体再退出，
	// 否则本体会因 setsid 独立会话而成为孤儿。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("收到退出信号：停止 mosdns 本体")
		_ = h.sup.stop()
		os.Exit(0)
	}()

	srv := &http.Server{Addr: addr, Handler: withToken(token, mux), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// api 管理接口。
type api struct {
	m     *mosdns.Manager
	sup   *supervisor
	token string
}

// withToken 校验 plugin token（内核反代时附带；见 ADR-039 §2）。
func withToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("X-Plugin-Token") != token {
			http.Error(w, "plugin token 无效", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- 进程托管（mosdns 本体）---

type supervisor struct{ dir string }

func (s *supervisor) bin() string  { return filepath.Join(s.dir, "mosdns") }
func (s *supervisor) pidF() string { return filepath.Join(s.dir, "mosdns.pid") }
func (s *supervisor) logF() string { return filepath.Join(s.dir, "mosdns.log") }

func (s *supervisor) installed() bool {
	_, err := os.Stat(s.bin())
	return err == nil
}

func (s *supervisor) pid() int {
	b, err := os.ReadFile(s.pidF())
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return p
}

func (s *supervisor) running() bool {
	p := s.pid()
	if p <= 0 {
		return false
	}
	if isZombie(p) {
		return false // 已退出但未回收：不可视为运行中
	}
	return syscall.Kill(p, 0) == nil
}

// isZombie 判定进程是否为僵尸（/proc/<pid>/stat 状态位为 Z）。
func isZombie(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return b[i+2] == 'Z'
}

func (s *supervisor) start() error {
	if s.running() {
		return nil
	}
	if !s.installed() {
		return fmt.Errorf("mosdns 未安装: %s", s.bin())
	}
	_ = os.Remove(s.pidF())
	lf, _ := os.OpenFile(s.logF(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	cmd := exec.Command(s.bin(), "start", "-d", s.dir, "-c", filepath.Join(s.dir, "config.yaml"))
	cmd.Dir = s.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if lf != nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	_ = os.WriteFile(s.pidF(), []byte(strconv.Itoa(pid)), 0o644)
	// 回收子进程，避免其退出后成为僵尸；退出后清理 pid 文件。
	go func() {
		_ = cmd.Wait()
		if s.pid() == pid {
			_ = os.Remove(s.pidF())
		}
	}()
	time.Sleep(300 * time.Millisecond)
	if !s.running() {
		return fmt.Errorf("mosdns 启动后立即退出，日志: %s", s.logF())
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

func (s *supervisor) restart() error {
	_ = s.stop()
	return s.start()
}

func (s *supervisor) version() string {
	if !s.installed() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.bin(), "version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// --- 状态 ---

type statusResp struct {
	Installed    bool   `json:"installed"`
	State        string `json:"state"`
	Healthy      bool   `json:"healthy"`
	Version      string `json:"version"`
	Listen       string `json:"listen"`
	APIAddr      string `json:"apiAddr"`
	ConfigPath   string `json:"configPath"`
	ConfigExists bool   `json:"configExists"`
	HostsPath    string `json:"hostsPath"`
	LogFile      string `json:"logFile"`
	CacheTag     string `json:"cacheTag"`
}

func (h *api) status(w http.ResponseWriter, _ *http.Request) {
	st := statusResp{
		Listen:     mosdns.DefaultListen,
		APIAddr:    h.m.APIAddr(),
		ConfigPath: h.m.ConfigPath(),
		HostsPath:  h.m.HostsPath(),
		LogFile:    h.m.LogFile(),
		CacheTag:   h.m.CacheTag(),
	}
	if _, err := h.m.ReadConfig(); err == nil {
		st.ConfigExists = true
	}
	if s, err := h.m.ReadSettings(); err == nil {
		st.Listen = s.Listen
	}
	st.Installed = h.sup.installed()
	st.Version = h.sup.version()
	if h.sup.running() {
		st.State = "running"
		st.Healthy = true
	} else if st.Installed {
		st.State = "stopped"
	} else {
		st.State = "unknown"
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *api) start(w http.ResponseWriter, _ *http.Request) {
	if err := h.sup.start(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "START_FAILED", err.Error())
		return
	}
	h.status(w, nil)
}

func (h *api) stop(w http.ResponseWriter, _ *http.Request) {
	if err := h.sup.stop(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "STOP_FAILED", err.Error())
		return
	}
	h.status(w, nil)
}

func (h *api) restart(w http.ResponseWriter, _ *http.Request) {
	if err := h.sup.restart(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "RESTART_FAILED", err.Error())
		return
	}
	h.status(w, nil)
}

// --- 设置 / 配置 ---

func (h *api) getSettings(w http.ResponseWriter, _ *http.Request) {
	s, err := h.m.ReadSettings()
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *api) putSettings(w http.ResponseWriter, r *http.Request) {
	var in mosdns.Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if msg := validateSettings(in); msg != "" {
		writeErrCode(w, http.StatusBadRequest, "INVALID_SETTINGS", msg)
		return
	}
	content, err := h.m.RenderConfig(in)
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "RENDER_FAILED", err.Error())
		return
	}
	if err := h.m.WriteConfig(content); err != nil {
		writeErrCode(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	if err := h.m.WriteSettings(in); err != nil {
		writeErrCode(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	s, _ := h.m.ReadSettings()
	writeJSON(w, http.StatusOK, s)
}

func validateSettings(s mosdns.Settings) string {
	if msg := validateListen(s.Listen); msg != "" {
		return msg
	}
	if len(s.LocalDNS) == 0 || len(s.RemoteDNS) == 0 {
		return "本地与远程上游 DNS 至少各配置一个"
	}
	if s.Cache && s.CacheSize <= 0 {
		return "缓存容量必须大于 0"
	}
	if s.Concurrent != 0 && (s.Concurrent < 1 || s.Concurrent > 3) {
		return "并发查询数需为 0(默认)或 1-3"
	}
	if s.EnableECSRomote && net.ParseIP(strings.TrimSpace(s.RemoteECSIP)) == nil {
		return "ECS 客户端子网 IP 不合法"
	}
	return ""
}

func validateListen(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "监听地址不能为空"
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return "监听地址格式应为 host:port 或 :port"
	}
	return ""
}

func (h *api) getConfig(w http.ResponseWriter, _ *http.Request) {
	content, err := h.m.ReadConfig()
	if errors.Is(err, mosdns.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, map[string]any{"content": "", "path": h.m.ConfigPath(), "configured": false})
		return
	}
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": string(content), "path": h.m.ConfigPath(), "configured": true})
}

func (h *api) putConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if err := h.m.WriteConfig([]byte(in.Content)); err != nil {
		writeErrCode(w, http.StatusBadRequest, "INVALID_CONFIG", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": h.m.ConfigPath()})
}

func (h *api) getHosts(w http.ResponseWriter, _ *http.Request) {
	hosts, err := h.m.ReadHosts()
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (h *api) putHosts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hosts []mosdns.Host `json:"hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if err := h.m.WriteHosts(in.Hosts); err != nil {
		writeErrCode(w, http.StatusBadRequest, "INVALID_HOSTS", err.Error())
		return
	}
	hosts, _ := h.m.ReadHosts()
	writeJSON(w, http.StatusOK, hosts)
}

// --- 规则 ---

func (h *api) listRules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, mosdns.GetRuleMeta())
}

func (h *api) getRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	content, err := h.m.ReadRule(name)
	if errors.Is(err, mosdns.ErrBadRule) {
		writeErrCode(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "content": content, "path": h.m.RulePath(name)})
}

func (h *api) putRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if err := h.m.WriteRule(name, in.Content); err != nil {
		if errors.Is(err, mosdns.ErrBadRule) {
			writeErrCode(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		writeErrCode(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "path": h.m.RulePath(name)})
}

// --- 数据库 ---

func (h *api) listGeodata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.m.GeoList())
}

func (h *api) updateGeodata(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	results := h.m.UpdateGeodata(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "items": h.m.GeoList()})
}

func (h *api) updateAdblock(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	results := h.m.UpdateAdSources(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// --- 日志 ---

func (h *api) getLogs(w http.ResponseWriter, _ *http.Request) {
	content, err := h.m.ReadLog(256 << 10)
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": content, "path": h.m.LogFile()})
}

func (h *api) clearLogs(w http.ResponseWriter, _ *http.Request) {
	if err := h.m.ClearLog(); err != nil {
		writeErrCode(w, http.StatusInternalServerError, "CLEAR_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *api) logsStream(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		conn.Read(ctx)
		cancel()
	}()

	f, err := os.Open(h.m.LogFile())
	if err != nil {
		sendLogErr(ctx, conn, "打开日志文件失败: "+err.Error())
		return
	}
	defer f.Close()

	tail := 500
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}
	for _, ln := range tailLines(f, tail) {
		if err := writeJSONMsg(ctx, conn, logMessage{Data: ln}); err != nil {
			return
		}
	}
	if r.URL.Query().Get("follow") == "false" {
		return
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return
	}
	reader := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return
		}
		if err := writeJSONMsg(ctx, conn, logMessage{Data: line}); err != nil {
			return
		}
	}
}

func (h *api) flush(w http.ResponseWriter, r *http.Request) {
	if err := h.m.FlushCache(r.Context()); err != nil {
		writeErrCode(w, http.StatusBadGateway, "FLUSH_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- 通用 helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

type logMessage struct {
	Stream string `json:"stream"`
	Data   string `json:"data"`
	Error  string `json:"error,omitempty"`
}

func writeJSONMsg(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func sendLogErr(ctx context.Context, conn *websocket.Conn, msg string) {
	_ = writeJSONMsg(ctx, conn, logMessage{Error: msg})
}

// tailLines 读文件尾部最后 n 行（不改动文件 offset）。
func tailLines(f *os.File, n int) []string {
	const chunk = 256 * 1024
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	start := size - chunk
	if start < 0 {
		start = 0
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(string(buf), "\n") {
		out = append(out, ln)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
