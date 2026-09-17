// Command tailscale-sidecar 是 GateBox 的独立进程插件后端：Tailscale 组网（attached）。
//
// Tailscale 属「系统提供（attached）」：GateBox **不安装、不更新其配方**，仅观测与控制
// 系统上的 `tailscale` CLI（对应 ADR-001/ADR-030 的 attached/system 语义）。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	dataDir := envOr("GATEBOX_DATA_DIR", ".")
	port := envOr("GATEBOX_PLUGIN_PORT", "8099")
	token := os.Getenv("GATEBOX_PLUGIN_TOKEN")

	// 运行目录（插件自身状态），与内核制品落点一致。
	dir := filepath.Join(dataDir, "tools", envOr("GATEBOX_PLUGIN_ID", "tailscale"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}

	s := &server{bin: resolveBin()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("POST /up", s.up)
	mux.HandleFunc("POST /down", s.down)

	// 收到信号直接退出（不托管任何子进程）。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; os.Exit(0) }()

	addr := "127.0.0.1:" + port
	log.Printf("tailscale 插件后端启动: %s, tailscale=%s", addr, s.bin)
	srv := &http.Server{Addr: addr, Handler: withToken(token, mux), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

type server struct{ bin string }

// resolveBin 定位系统 tailscale 可执行文件（PATH 或 TAILSCALE_BIN）。
func resolveBin() string {
	if v := strings.TrimSpace(os.Getenv("TAILSCALE_BIN")); v != "" {
		return v
	}
	for _, p := range []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	return ""
}

// tsStatus 对应 `tailscale status --json` 的部分字段。
type tsStatus struct {
	BackendState string `json:"BackendState"`
	Version      string `json:"Version"`
	Self         struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
		DNSName      string   `json:"DNSName"`
	} `json:"Self"`
}

func (s *server) status(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{
		"installed": s.bin != "",
		"state":     "unknown",
		"healthy":   false,
		"version":   "",
		"ips":       []string{},
		"dnsName":   "",
		"message":   "",
	}
	if s.bin == "" {
		out["message"] = "未找到 tailscale 命令（系统未安装）"
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["version"] = s.version()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, s.bin, "status", "--json").Output()
	if err != nil {
		out["message"] = "tailscale status 失败: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	var st tsStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		out["message"] = "解析 tailscale status 失败: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["backendState"] = st.BackendState
	out["ips"] = st.Self.TailscaleIPs
	out["dnsName"] = strings.TrimSuffix(st.Self.DNSName, ".")
	switch st.BackendState {
	case "Running":
		out["state"] = "running"
		out["healthy"] = true
	case "Stopped":
		out["state"] = "stopped"
	case "NeedsLogin":
		out["state"] = "needs-login"
		out["message"] = "需要登录：请在终端执行 tailscale up 并完成授权"
	default:
		out["state"] = "unknown"
		out["message"] = "后端状态: " + st.BackendState
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) version() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.bin, "version").Output()
	if err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return line
}

func (s *server) up(w http.ResponseWriter, r *http.Request) {
	s.control(w, r, "up")
}

func (s *server) down(w http.ResponseWriter, r *http.Request) {
	s.control(w, r, "down")
}

// control 执行 tailscale up/down；耗时较长（up 可能等待授权），返回输出。
func (s *server) control(w http.ResponseWriter, r *http.Request, action string) {
	if s.bin == "" {
		writeErrCode(w, http.StatusBadRequest, "NOT_INSTALLED", "系统未安装 tailscale")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.bin, action)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeErrCode(w, http.StatusBadRequest, "CONTROL_FAILED", fmt.Sprintf("%s 失败: %v\n%s", action, err, out))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out, "message": "已执行 tailscale " + action})
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
