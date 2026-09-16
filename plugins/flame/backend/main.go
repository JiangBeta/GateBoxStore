// Command flame-sidecar 是 GateBox 的独立进程插件后端：应用导航面板（flame）。
//
// 代管 flame 本体进程（ADR-037 决策 (a)），并暴露状态供内嵌页使用。
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	dataDir := envOr("GATEBOX_DATA_DIR", ".")
	port := envOr("GATEBOX_PLUGIN_PORT", "8099")
	token := os.Getenv("GATEBOX_PLUGIN_TOKEN")
	flamePort := envOr("GATEBOX_FLAME_PORT", "5005")

	dir := filepath.Join(dataDir, "tools", "flame")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}
	s := &supervisor{dir: dir, port: flamePort}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("POST /start", s.startH)
	mux.HandleFunc("POST /stop", s.stopH)
	mux.HandleFunc("POST /restart", s.restart)

	addr := "127.0.0.1:" + port
	log.Printf("flame 插件后端启动: %s, 运行目录 %s", addr, dir)
	// 收到信号时先停 flame 本体再退出，避免孤儿进程。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("收到退出信号：停止 flame 本体")
		_ = s.stop()
		os.Exit(0)
	}()

	srv := &http.Server{Addr: addr, Handler: withToken(token, mux), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

type supervisor struct {
	dir  string
	port string
}

func (s *supervisor) bin() string  { return filepath.Join(s.dir, "flame") }
func (s *supervisor) pidF() string { return filepath.Join(s.dir, "flame.pid") }
func (s *supervisor) logF() string { return filepath.Join(s.dir, "flame.log") }

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

func (s *supervisor) start() error {
	if s.running() {
		return nil
	}
	if !s.installed() {
		return fmt.Errorf("flame 未安装: %s", s.bin())
	}
	_ = os.Remove(s.pidF())
	lf, _ := os.OpenFile(s.logF(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	cmd := exec.Command(s.bin())
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), "PORT="+s.port)
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
		return fmt.Errorf("flame 启动后立即退出，日志: %s", s.logF())
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

func (s *supervisor) status(w http.ResponseWriter, _ *http.Request) {
	state := "unknown"
	if s.running() {
		state = "running"
	} else if s.installed() {
		state = "stopped"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installed": s.installed(),
		"state":     state,
		"healthy":   s.running(),
		"port":      s.port,
		"webPath":   "/",
	})
}

func (s *supervisor) startH(w http.ResponseWriter, r *http.Request) {
	if err := s.start(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "START_FAILED", err.Error())
		return
	}
	s.status(w, r)
}

func (s *supervisor) stopH(w http.ResponseWriter, r *http.Request) {
	if err := s.stop(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "STOP_FAILED", err.Error())
		return
	}
	s.status(w, r)
}

func (s *supervisor) restart(w http.ResponseWriter, r *http.Request) {
	_ = s.stop()
	if err := s.start(); err != nil {
		writeErrCode(w, http.StatusBadRequest, "RESTART_FAILED", err.Error())
		return
	}
	s.status(w, r)
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
