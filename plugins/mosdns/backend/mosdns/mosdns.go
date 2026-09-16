// Package mosdns 读写外部 mosdns 组件的运行配置(ADR-001:只改写配置 + 调 API)。
//
// 覆盖:
//   - 基础/高级/Cloudflare 设置表单 ⇄ config.yaml 生成(参照 sbwml/luci-app-mosdns);
//   - 规则列表(whitelist/blocklist/greylist/ddnslist/redirect/local-ptr/streaming/adblock/cloudflare-cidr)读写;
//   - 内网解析记录(hosts 插件引用的 hosts.txt)增删改;
//   - GeoIP/GeoSite 数据库更新(下载社区维护的纯文本列表);
//   - 运行状态、日志读取与缓存刷新。
//
// mosdns 由 GateBox 组件运行时启动(component/registry.go 的 pid 托管),
// 本包只负责配置文件,不接管进程生命周期。
//
// 与 luci-app-mosdns 的差异:luci 使用 sbwml 打过补丁的 mosdns(adblock_set / stats_api
// 插件);GateBox 使用官方 mosdns 制品,故广告/拦截规则统一用官方 domain_set 承载,
// 规则文件须为 mosdns 域名规则(plain / domain: / full: / keyword: / regexp:)格式。
package mosdns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 默认值与文件名。配置相对路径以 mosdns 工作目录(--dir)为基准。
const (
	DefaultListen    = "0.0.0.0:5335"
	DefaultAPIPort   = 9091
	defaultLogLevel  = "error"
	defaultCacheCap  = 8000
	defaultCacheTTL  = 86400
	defaultDumpEvery = 3600
	defaultIdle      = 30
	defaultConc      = 2
	defaultBootstrap = "119.29.29.29"

	configFileName   = "config.yaml"
	settingsFileName = "settings.json"
	hostsFileName    = "hosts.txt"
	logFileName      = "mosdns.log"
	cacheDumpName    = "cache.dump"
	ruleDirName      = "rule"
	adlistDirName    = "rule/adlist"
)

// Settings 页面表单模型。写入 settings.json,并据此生成 config.yaml。
type Settings struct {
	Listen   string `json:"listen"`   // 0.0.0.0:5335
	LogLevel string `json:"logLevel"` // debug | info | warn | error

	// 上游与转发
	LocalDNS   []string `json:"localDns"`   // 本地/国内上游
	RemoteDNS  []string `json:"remoteDns"`  // 远程/国外上游
	StreamDNS  []string `json:"streamDns"`  // 流媒体上游
	Bootstrap  string   `json:"bootstrap"`  // DoH/DoT 域名引导 DNS
	Concurrent int      `json:"concurrent"` // 并发查询数 1-3

	IdleTimeout        int    `json:"idleTimeout"`        // 连接复用空闲超时(秒)
	EnablePipeline     bool   `json:"enablePipeline"`     // TCP/DoT 连接复用
	InsecureSkipVerify bool   `json:"insecureSkipVerify"` // 跳过上游 TLS 校验
	EnableECSRomote    bool   `json:"enableEcsRemote"`    // 远程上游启用 ECS
	RemoteECSIP        string `json:"remoteEcsIp"`        // ECS 客户端子网 IP
	DNSLeak            bool   `json:"dnsLeak"`            // 防止 DNS 泄漏(primary 直接用远程)
	PreferIPv4CN       bool   `json:"preferIpv4Cn"`       // 国内解析优先 IPv4
	PreferIPv4         bool   `json:"preferIpv4"`         // 远程解析优先 IPv4

	AppleOptimization    bool `json:"appleOptimization"`    // Apple 域名优化
	CustomStreamMediaDNS bool `json:"customStreamMediaDns"` // 自定义流媒体 DNS

	// 缓存
	Cache        bool `json:"cache"`
	CacheSize    int  `json:"cacheSize"`
	LazyCacheTTL int  `json:"lazyCacheTtl"`
	DumpFile     bool `json:"dumpFile"`
	DumpInterval int  `json:"dumpInterval"`

	// TTL 与过滤
	MinimalTTL   int      `json:"minimalTtl"`
	MaximumTTL   int      `json:"maximumTtl"`
	RejectType65 bool     `json:"rejectType65"`
	Adblock      bool     `json:"adblock"`
	AdSources    []string `json:"adSources"` // 广告规则来源(http(s):// 或本地路径)

	// Cloudflare
	Cloudflare   bool     `json:"cloudflare"`
	CloudflareIP []string `json:"cloudflareIp"`

	// 数据库更新
	GeoProxy string `json:"geoProxy"` // GitHub 代理前缀(可空)
}

// Host 一条内网解析记录(hosts 插件):一个域名对应一个或多个 IP。
type Host struct {
	Domain string   `json:"domain"`
	IPs    []string `json:"ips"`
}

// RuleMeta 规则列表描述。
type RuleMeta struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Help  string `json:"help"`
}

// ruleSpecs 规则列表清单(name → 文件 + 说明)。
var ruleSpecs = []RuleMeta{
	{Name: "whitelist", Label: "白名单", Help: "强制走本地 DNS(最高优先级),每行一条域名规则"},
	{Name: "blocklist", Label: "黑名单", Help: "命中即拦截(black_hole + reject)"},
	{Name: "greylist", Label: "灰名单", Help: "强制走远程 DNS"},
	{Name: "ddnslist", Label: "DDNS 名单", Help: "强制走本地 DNS,TTL 固定 5s 且不缓存"},
	{Name: "redirect", Label: "域名重定向", Help: "把请求域名 A 重定向到域名 B,如 baidu.com qq.com"},
	{Name: "local-ptr", Label: "PTR 拦截", Help: "拦截 PTR 查询"},
	{Name: "streaming", Label: "流媒体", Help: "命中后使用「流媒体 DNS」解析"},
	{Name: "adblock", Label: "广告拦截", Help: "命中即拦截(需在高级设置启用广告拦截)"},
	{Name: "cloudflare-cidr", Label: "Cloudflare IP 段", Help: "匹配后改写为自定义 IP(需启用 Cloudflare)"},
}

// ruleFileName 规则 name → 相对文件名;未知返回空。
func ruleFileName(name string) string {
	for _, r := range ruleSpecs {
		if r.Name == name {
			switch name {
			case "cloudflare-cidr":
				return ruleDirName + "/cloudflare-cidr.txt"
			default:
				return ruleDirName + "/" + name + ".txt"
			}
		}
	}
	return ""
}

// geoFileNames 配置引用的 GeoIP/GeoSite 数据文件(缺失时以空文件兜底)。
var geoFileNames = []string{
	"geosite_cn.txt",
	"geoip_cn.txt",
	"geosite_apple.txt",
	"geosite_geolocation-!cn.txt",
}

// geoSources 数据库更新来源(纯文本,mosdns 可直接读取)。
var geoSources = []struct {
	Name  string
	Label string
	URL   string
}{
	{"geosite_cn.txt", "国内域名", "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"},
	{"geosite_geolocation-!cn.txt", "国外域名", "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"},
	{"geosite_apple.txt", "Apple 域名", "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt"},
	{"geoip_cn.txt", "国内 IP", "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt"},
}

// GeoItem 数据库文件状态。
type GeoItem struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// GeoResult 单个文件更新结果。
type GeoResult struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Size  int64  `json:"size"`
	Error string `json:"error,omitempty"`
}

// Manager mosdns 配置管理:运行目录固定为 $DATA_DIR/tools/mosdns。
type Manager struct {
	dir string
}

// NewManager 构造 Manager,dir 为 mosdns 运行目录(不存在时惰性创建)。
func NewManager(dir string) *Manager {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &Manager{dir: dir}
}

// Dir mosdns 运行目录。
func (m *Manager) Dir() string { return m.dir }

// ConfigPath config.yaml 路径。
func (m *Manager) ConfigPath() string { return filepath.Join(m.dir, configFileName) }

// settingsPath settings.json 路径。
func (m *Manager) settingsPath() string { return filepath.Join(m.dir, settingsFileName) }

// HostsPath hosts 文件路径(默认 hosts.txt;若配置指定则按其解析)。
func (m *Manager) HostsPath() string {
	if b, err := m.ReadConfig(); err == nil {
		var raw rawConfig
		if yaml.Unmarshal(b, &raw) == nil {
			for _, p := range raw.Plugins {
				if p.Type != "hosts" {
					continue
				}
				if files, ok := argsMap(p.Args)["files"].([]any); ok && len(files) > 0 {
					if f, ok := files[0].(string); ok && strings.TrimSpace(f) != "" {
						if filepath.IsAbs(f) {
							return f
						}
						return filepath.Join(m.dir, f)
					}
				}
			}
		}
	}
	return filepath.Join(m.dir, hostsFileName)
}

// RulePath 规则文件的绝对路径;name 非法返回空。
func (m *Manager) RulePath(name string) string {
	rel := ruleFileName(name)
	if rel == "" {
		return ""
	}
	return filepath.Join(m.dir, filepath.FromSlash(rel))
}

// GetRuleMeta 返回规则清单。
func GetRuleMeta() []RuleMeta { return append([]RuleMeta{}, ruleSpecs...) }

// DefaultSettings 默认设置。
func DefaultSettings() Settings {
	return Settings{
		Listen:       DefaultListen,
		LogLevel:     defaultLogLevel,
		LocalDNS:     []string{"223.5.5.5", "119.29.29.29"},
		RemoteDNS:    []string{"tls://8.8.8.8", "tls://1.1.1.1"},
		StreamDNS:    []string{"tls://8.8.8.8"},
		Bootstrap:    defaultBootstrap,
		Concurrent:   defaultConc,
		IdleTimeout:  defaultIdle,
		Cache:        true,
		CacheSize:    defaultCacheCap,
		LazyCacheTTL: defaultCacheTTL,
		DumpInterval: defaultDumpEvery,
	}
}

// --- 配置生成 ---

type yamlLog map[string]any

type plugin struct {
	Tag  string `yaml:"tag"`
	Type string `yaml:"type"`
	Args any    `yaml:"args"`
}

type fileConfig struct {
	Log     yamlLog  `yaml:"log"`
	API     yamlLog  `yaml:"api"`
	Include []string `yaml:"include"`
	Plugins []plugin `yaml:"plugins"`
}

func domSet(tag string, files []string) plugin {
	return plugin{Tag: tag, Type: "domain_set", Args: map[string]any{"files": files}}
}

func ipSet(tag string, files []string) plugin {
	return plugin{Tag: tag, Type: "ip_set", Args: map[string]any{"files": files}}
}

func seqStep(exec string) map[string]any { return map[string]any{"exec": exec} }

func seqStepMatch(matches any, exec string) map[string]any {
	return map[string]any{"matches": matches, "exec": exec}
}

func seqPlugin(tag string, steps []any) plugin {
	return plugin{Tag: tag, Type: "sequence", Args: steps}
}

func fallbackPlugin(tag, primary, secondary string, threshold int) plugin {
	return plugin{Tag: tag, Type: "fallback", Args: map[string]any{
		"primary": primary, "secondary": secondary, "threshold": threshold, "always_standby": true,
	}}
}

// RenderConfig 依设置生成完整的 mosdns config.yaml。
func (m *Manager) RenderConfig(s Settings) ([]byte, error) {
	s = normalizeSettings(s)

	logFile := m.defaultLogPath()
	plugins := []plugin{
		domSet("geosite_cn", []string{"geosite_cn.txt"}),
		ipSet("geoip_cn", []string{"geoip_cn.txt"}),
		domSet("geosite_apple", []string{"geosite_apple.txt"}),
		domSet("geosite_no_cn", []string{"geosite_geolocation-!cn.txt"}),
		domSet("whitelist", []string{"rule/whitelist.txt"}),
		domSet("blocklist", []string{"rule/blocklist.txt"}),
		domSet("greylist", []string{"rule/greylist.txt"}),
		domSet("ddnslist", []string{"rule/ddnslist.txt"}),
		{Tag: "hosts", Type: "hosts", Args: map[string]any{"files": []string{"hosts.txt"}}},
		{Tag: "redirect", Type: "redirect", Args: map[string]any{"files": []string{"rule/redirect.txt"}}},
		domSet("adlist", m.adlistFiles()),
		domSet("local_ptr", []string{"rule/local-ptr.txt"}),
		domSet("stream_media", []string{"rule/streaming.txt"}),
		ipSet("cloudflare_cidr", []string{"rule/cloudflare-cidr.txt"}),
	}
	if s.Cache {
		plugins = append(plugins, cachePlugin(s))
	}
	plugins = append(plugins,
		forwardPlugin("forward_xinfeng_udp", []string{"114.114.114.114", "114.114.115.115"}, s),
		forwardPlugin("forward_local", s.LocalDNS, s),
		forwardPlugin("forward_remote", s.RemoteDNS, s),
		seqPlugin("forward_remote_upstream", remoteUpstreamSteps(s)),
	)
	if s.CustomStreamMediaDNS {
		plugins = append(plugins,
			forwardPlugin("forward_stream_media", s.StreamDNS, s),
			seqPlugin("forward_stream_media_upstream", remoteUpstreamSteps(s)),
		)
	}
	if s.MinimalTTL > 0 || s.MaximumTTL > 0 {
		plugins = append(plugins, seqPlugin("modify_ttl", []any{seqStep(fmt.Sprintf("ttl %d-%d", s.MinimalTTL, s.MaximumTTL))}))
	}
	plugins = append(plugins,
		seqPlugin("modify_ddns_ttl", []any{seqStep("ttl 5-5")}),
		seqPlugin("has_resp_sequence", m.hasRespSteps(s)),
		seqPlugin("query_is_non_local_ip", []any{
			seqStep("$forward_local"),
			seqStepMatch("!resp_ip $geoip_cn", "drop_resp"),
		}),
		m.fallbackPlugin(s),
		fallbackPlugin("apple_domain_fallback", "query_is_non_local_ip", "forward_xinfeng_udp", 100),
		seqPlugin("query_is_apple_domain", []any{
			seqStepMatch("!qname $geosite_apple", "return"),
			seqStep("$apple_domain_fallback"),
		}),
		seqPlugin("query_is_ddns_domain", []any{seqStepMatch("qname $ddnslist", "$forward_local")}),
		seqPlugin("query_is_local_domain", queryLocalSteps("$geosite_cn", s)),
		seqPlugin("query_is_no_local_domain", []any{seqStepMatch("qname $geosite_no_cn", "$forward_remote_upstream")}),
		seqPlugin("query_is_whitelist_domain", queryLocalSteps("$whitelist", s)),
		seqPlugin("query_is_greylist_domain", []any{seqStepMatch("qname $greylist", "$forward_remote_upstream")}),
		seqPlugin("query_is_reject_domain", m.rejectSteps(s)),
	)
	if s.CustomStreamMediaDNS {
		plugins = append(plugins, seqPlugin("query_is_stream_media_domain", []any{
			seqStepMatch("qname $stream_media", "$forward_stream_media_upstream"),
		}))
	}
	plugins = append(plugins,
		seqPlugin("main_sequence", m.mainSteps(s)),
		plugin{Tag: "udp_server", Type: "udp_server", Args: map[string]any{"entry": "main_sequence", "listen": s.Listen}},
		plugin{Tag: "tcp_server", Type: "tcp_server", Args: map[string]any{"entry": "main_sequence", "listen": s.Listen}},
	)

	cfg := fileConfig{
		Log:     yamlLog{"level": s.LogLevel, "file": logFile},
		API:     yamlLog{"http": fmt.Sprintf("127.0.0.1:%d", DefaultAPIPort)},
		Include: []string{},
		Plugins: plugins,
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

// cachePlugin 生成缓存插件(按需附带落盘参数)。
// 注:官方 mosdns 的 cache 不支持 prefetch 系列参数(见 sbwml patch),故不生成。
func cachePlugin(s Settings) plugin {
	args := map[string]any{"size": s.CacheSize, "lazy_cache_ttl": s.LazyCacheTTL}
	if s.DumpFile {
		args["dump_file"] = cacheDumpName
		args["dump_interval"] = s.DumpInterval
	}
	return plugin{Tag: "cache", Type: "cache", Args: args}
}

// forwardPlugin 生成 forward 插件(h3:// 自动转 https:// 并开 http3)。
func forwardPlugin(tag string, addrs []string, s Settings) plugin {
	ups := make([]any, 0, len(addrs))
	for _, raw := range addrs {
		a := strings.TrimSpace(raw)
		if a == "" {
			continue
		}
		up := map[string]any{
			"addr":                 a,
			"bootstrap":            s.Bootstrap,
			"enable_pipeline":      s.EnablePipeline,
			"insecure_skip_verify": s.InsecureSkipVerify,
			"idle_timeout":         s.IdleTimeout,
		}
		if strings.HasPrefix(a, "h3://") {
			up["addr"] = "https://" + strings.TrimPrefix(a, "h3://")
			up["enable_http3"] = true
		}
		ups = append(ups, up)
	}
	return plugin{Tag: tag, Type: "forward", Args: map[string]any{"concurrent": s.Concurrent, "upstreams": ups}}
}

// remoteUpstreamSteps 远程/流媒体上游 sequence(prefer_ipv4 + ecs 可选)。
func remoteUpstreamSteps(s Settings) []any {
	steps := []any{}
	if s.PreferIPv4 {
		steps = append(steps, seqStep("prefer_ipv4"))
	}
	if s.EnableECSRomote && strings.TrimSpace(s.RemoteECSIP) != "" {
		steps = append(steps, seqStep("ecs "+strings.TrimSpace(s.RemoteECSIP)))
	}
	steps = append(steps, seqStep("$forward_remote"))
	return steps
}

// queryLocalSteps 本地解析 sequence(国内按需 prefer_ipv4)。
func queryLocalSteps(set string, s Settings) []any {
	steps := []any{}
	if s.PreferIPv4CN {
		steps = append(steps, seqStep("prefer_ipv4"))
	}
	steps = append(steps, seqStepMatch("qname "+set, "$forward_local"))
	return steps
}

// hasRespSteps 有应答终止序列:TTL 修正 + Cloudflare 改写 + accept。
func (m *Manager) hasRespSteps(s Settings) []any {
	steps := []any{seqStepMatch("qname $ddnslist", "$modify_ddns_ttl")}
	if s.MinimalTTL > 0 || s.MaximumTTL > 0 {
		steps = append(steps, seqStepMatch(
			[]string{"!qname $ddnslist", "!qname $blocklist", "!qname $adlist"}, "$modify_ttl"))
	}
	if s.Cloudflare && len(s.CloudflareIP) > 0 {
		steps = append(steps, seqStepMatch(
			[]string{"!qname $whitelist", "!qname $greylist", "!qname $stream_media", "resp_ip $cloudflare_cidr"},
			"black_hole "+strings.Join(s.CloudflareIP, " ")))
	}
	steps = append(steps, seqStepMatch("has_resp", "accept"))
	return steps
}

// fallbackPlugin 主 fallback(可选防泄漏)。
func (m *Manager) fallbackPlugin(s Settings) plugin {
	primary := "query_is_non_local_ip"
	if s.DNSLeak {
		primary = "forward_remote_upstream"
	}
	return fallbackPlugin("fallback", primary, "forward_remote_upstream", 500)
}

// rejectSteps 黑名单/广告/PTR/HTTPS 记录拦截。
func (m *Manager) rejectSteps(s Settings) []any {
	steps := []any{
		seqStepMatch("qname $blocklist", "black_hole 0.0.0.0 ::"),
	}
	if s.Adblock {
		steps = append(steps, seqStepMatch("qname $adlist", "black_hole 0.0.0.0 ::"))
	}
	steps = append(steps,
		seqStepMatch("has_resp", "ttl 3600"),
		seqStepMatch("has_resp", "accept"),
		seqStepMatch("qname $blocklist", "reject 3"),
	)
	if s.Adblock {
		steps = append(steps, seqStepMatch("qname $adlist", "reject 3"))
	}
	steps = append(steps, seqStepMatch([]string{"qtype 12", "qname $local_ptr"}, "reject 3"))
	if s.RejectType65 {
		steps = append(steps, seqStepMatch("qtype 65", "reject 3"))
	}
	return steps
}

// mainSteps 主运行序列。
func (m *Manager) mainSteps(s Settings) []any {
	steps := []any{
		seqStep("$hosts"),
		seqStep("jump has_resp_sequence"),
	}
	if s.Cache {
		steps = append(steps,
			seqStepMatch([]string{"!qname $ddnslist", "!qname $blocklist", "!qname $adlist", "!qname $local_ptr"}, "$cache"),
			seqStep("jump has_resp_sequence"),
		)
	}
	steps = append(steps,
		seqStep("$redirect"),
		seqStep("jump has_resp_sequence"),
	)
	if s.AppleOptimization {
		steps = append(steps,
			seqStep("$query_is_apple_domain"),
			seqStep("jump has_resp_sequence"),
		)
	}
	steps = append(steps,
		seqStep("$query_is_ddns_domain"),
		seqStep("jump has_resp_sequence"),
		seqStep("$query_is_whitelist_domain"),
		seqStep("jump has_resp_sequence"),
		seqStep("$query_is_reject_domain"),
		seqStep("jump has_resp_sequence"),
		seqStep("$query_is_greylist_domain"),
		seqStep("jump has_resp_sequence"),
	)
	if s.CustomStreamMediaDNS {
		steps = append(steps,
			seqStep("$query_is_stream_media_domain"),
			seqStep("jump has_resp_sequence"),
		)
	}
	steps = append(steps,
		seqStep("$query_is_local_domain"),
		seqStep("jump has_resp_sequence"),
		seqStep("$query_is_no_local_domain"),
		seqStep("jump has_resp_sequence"),
		seqStep("$fallback"),
		seqStep("jump has_resp_sequence"),
	)
	return steps
}

// adlistFiles 广告拦截数据文件:手动 adblock.txt + 已下载的 rule/adlist/*.txt。
func (m *Manager) adlistFiles() []string {
	files := []string{"rule/adblock.txt"}
	entries, err := os.ReadDir(filepath.Join(m.dir, adlistDirName))
	if err != nil {
		return files
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		files = append(files, adlistDirName+"/"+n)
	}
	return files
}

// normalizeSettings 补全空字段的默认值。
func normalizeSettings(s Settings) Settings {
	def := DefaultSettings()
	if strings.TrimSpace(s.Listen) == "" {
		s.Listen = def.Listen
	}
	switch s.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		s.LogLevel = def.LogLevel
	}
	if len(s.LocalDNS) == 0 {
		s.LocalDNS = def.LocalDNS
	}
	if len(s.RemoteDNS) == 0 {
		s.RemoteDNS = def.RemoteDNS
	}
	if strings.TrimSpace(s.Bootstrap) == "" {
		s.Bootstrap = def.Bootstrap
	}
	if s.Concurrent <= 0 {
		s.Concurrent = def.Concurrent
	}
	if s.IdleTimeout <= 0 {
		s.IdleTimeout = def.IdleTimeout
	}
	if s.CacheSize <= 0 {
		s.CacheSize = def.CacheSize
	}
	if s.LazyCacheTTL < 0 {
		s.LazyCacheTTL = 0
	}
	if s.DumpInterval <= 0 {
		s.DumpInterval = def.DumpInterval
	}
	if s.CustomStreamMediaDNS && len(s.StreamDNS) == 0 {
		s.StreamDNS = def.StreamDNS
	}
	return s
}

// --- 设置读写(settings.json) ---

// ReadSettings 读取表单设置:settings.json 优先,缺失时从 config.yaml 解析,再回退默认。
func (m *Manager) ReadSettings() (Settings, error) {
	if b, err := os.ReadFile(m.settingsPath()); err == nil {
		var s Settings
		if json.Unmarshal(b, &s) == nil {
			return normalizeSettings(s), nil
		}
	}
	if b, err := m.ReadConfig(); err == nil {
		if s, perr := ParseSettings(b); perr == nil {
			return normalizeSettings(s), nil
		}
	}
	return DefaultSettings(), nil
}

// WriteSettings 保存表单设置为 settings.json。
func (m *Manager) WriteSettings(s Settings) error {
	if err := m.ensureDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.settingsPath(), append(b, '\n'), 0o644)
}

// --- 配置读写 ---

// ReadConfig 读取 config.yaml 原文;文件不存在返回 ErrNotConfigured。
func (m *Manager) ReadConfig() ([]byte, error) {
	b, err := os.ReadFile(m.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	return b, err
}

// WriteConfig 校验并原子写入 config.yaml(保留一份 .bak 备份)。
func (m *Manager) WriteConfig(b []byte) error {
	if err := ValidateConfig(b); err != nil {
		return err
	}
	if err := m.ensureDataFiles(); err != nil {
		return err
	}
	if old, err := os.ReadFile(m.ConfigPath()); err == nil && !bytes.Equal(old, b) {
		_ = os.WriteFile(m.ConfigPath()+".bak", old, 0o644)
	}
	return atomicWrite(m.ConfigPath(), b)
}

// ValidateConfig 校验 config.yaml 至少是合法 YAML 且插件 tag/type 齐备。
func ValidateConfig(b []byte) error {
	if len(strings.TrimSpace(string(b))) == 0 {
		return errors.New("配置内容为空")
	}
	var raw rawConfig
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("YAML 解析失败: %w", err)
	}
	if len(raw.Plugins) == 0 {
		return errors.New("配置缺少 plugins 列表")
	}
	seen := map[string]bool{}
	for i, p := range raw.Plugins {
		if strings.TrimSpace(p.Tag) == "" {
			return fmt.Errorf("第 %d 个插件缺少 tag", i+1)
		}
		if strings.TrimSpace(p.Type) == "" {
			return fmt.Errorf("插件 %s 缺少 type", p.Tag)
		}
		if seen[p.Tag] {
			return fmt.Errorf("插件 tag 重复: %s", p.Tag)
		}
		seen[p.Tag] = true
	}
	return nil
}

// --- 解析已有配置 ---

type rawConfig struct {
	Log struct {
		Level string `yaml:"level"`
		File  string `yaml:"file"`
	} `yaml:"log"`
	API struct {
		HTTP string `yaml:"http"`
	} `yaml:"api"`
	Plugins []struct {
		Tag  string `yaml:"tag"`
		Type string `yaml:"type"`
		Args any    `yaml:"args"`
	} `yaml:"plugins"`
}

// argsMap 把插件 args 归一为映射(非映射返回 nil)。
func argsMap(args any) map[string]any {
	m, _ := args.(map[string]any)
	return m
}

// ParseSettings 尽力从 config.yaml 还原设置(用于 settings.json 缺失时的导入)。
func ParseSettings(b []byte) (Settings, error) {
	s := DefaultSettings()
	var raw rawConfig
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return s, fmt.Errorf("YAML 解析失败: %w", err)
	}
	if lvl := strings.TrimSpace(raw.Log.Level); lvl != "" {
		s.LogLevel = lvl
	}
	s.Cache = false
	for _, p := range raw.Plugins {
		args := argsMap(p.Args)
		switch {
		case p.Type == "cache":
			s.Cache = true
			if n := argInt(args, "size"); n > 0 {
				s.CacheSize = n
			}
			if n := argInt(args, "lazy_cache_ttl"); n >= 0 {
				s.LazyCacheTTL = n
			}
			if _, ok := args["dump_file"]; ok {
				s.DumpFile = true
				if n := argInt(args, "dump_interval"); n > 0 {
					s.DumpInterval = n
				}
			}
		case p.Tag == "forward_local":
			if u := argUpstreams(args); len(u) > 0 {
				s.LocalDNS = u
			}
			if n := argInt(args, "concurrent"); n > 0 {
				s.Concurrent = n
			}
		case p.Tag == "forward_remote":
			if u := argUpstreams(args); len(u) > 0 {
				s.RemoteDNS = u
			}
		case p.Tag == "forward_stream_media":
			if u := argUpstreams(args); len(u) > 0 {
				s.StreamDNS = u
			}
		case p.Type == "udp_server" || p.Type == "tcp_server":
			if l := argString(args, "listen"); l != "" {
				s.Listen = l
			}
		case p.Tag == "adlist":
			s.Adblock = true
		case p.Tag == "cloudflare_cidr":
			// Cloudflare 是否启用由 has_resp_sequence 的 black_hole 步骤决定,难以静态判定;
			// 存在该 ip_set 时保守视为启用,由 settings.json 覆盖。
			s.Cloudflare = true
		}
	}
	return s, nil
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argInt(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case uint64:
		return int(v)
	default:
		return 0
	}
}

func argUpstreams(args map[string]any) []string {
	if args == nil {
		return nil
	}
	list, ok := args["upstreams"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if addr, ok := m["addr"].(string); ok && strings.TrimSpace(addr) != "" {
				out = append(out, strings.TrimSpace(addr))
			}
		}
	}
	return out
}

// --- 目录与数据文件 ---

func (m *Manager) ensureDir() error {
	if err := os.MkdirAll(filepath.Join(m.dir, ruleDirName), 0o755); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(m.dir, adlistDirName), 0o755)
}

// EnsureLayout 确保运行目录与配置引用的数据/规则文件存在(缺失建空文件)。
// 供进程启动前调用:mosdns 会因引用的文件缺失而拒绝启动。
func (m *Manager) EnsureLayout() error { return m.ensureDataFiles() }

// ensureDataFiles 确保配置引用的数据/规则文件存在(缺失则建空文件,保证 mosdns 可启动)。
func (m *Manager) ensureDataFiles() error {
	if err := m.ensureDir(); err != nil {
		return err
	}
	for _, f := range append([]string{hostsFileName}, geoFileNames...) {
		if err := touchFile(filepath.Join(m.dir, f), ""); err != nil {
			return err
		}
	}
	for _, r := range ruleSpecs {
		path := m.RulePath(r.Name)
		if path == "" {
			continue
		}
		if err := touchFile(path, "# "+r.Label+"("+r.Help+")\n"); err != nil {
			return err
		}
	}
	return nil
}

// touchFile 文件不存在时创建并写入 header。
func touchFile(path, header string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(header), 0o644)
}

// atomicWrite 先写临时文件再改名,避免读到半截内容。
func atomicWrite(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_ = tmp.Chmod(0o644)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// --- 规则列表 ---

// ReadRule 读取规则文件;不存在返回空串。
func (m *Manager) ReadRule(name string) (string, error) {
	path := m.RulePath(name)
	if path == "" {
		return "", ErrBadRule
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// WriteRule 写入规则文件(先建目录+数据文件兜底)。
func (m *Manager) WriteRule(name, content string) error {
	path := m.RulePath(name)
	if path == "" {
		return ErrBadRule
	}
	if err := m.ensureDir(); err != nil {
		return err
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return atomicWrite(path, []byte(content))
}

// --- 内网解析记录(hosts) ---

// ReadHosts 读取 hosts 文件;文件不存在返回空列表。
func (m *Manager) ReadHosts() ([]Host, error) {
	b, err := os.ReadFile(m.HostsPath())
	if errors.Is(err, os.ErrNotExist) {
		return []Host{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseHosts(string(b)), nil
}

// parseHosts 解析 hosts 文本(mosdns hosts 格式:域名在前,IP 在后,支持一行多个)。
func parseHosts(content string) []Host {
	out := []Host{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, Host{Domain: fields[0], IPs: append([]string{}, fields[1:]...)})
	}
	return out
}

// WriteHosts 校验并原子写入 hosts 文件(按 mosdns hosts 格式)。
func (m *Manager) WriteHosts(hosts []Host) error {
	var sb strings.Builder
	sb.WriteString("# GateBox 内网解析记录(域名 IP...,支持一行多个 IP / IPv6)\n")
	for _, h := range hosts {
		domain := strings.TrimSpace(h.Domain)
		if domain == "" {
			return errors.New("域名不能为空")
		}
		if strings.ContainsAny(domain, " \t") {
			return fmt.Errorf("域名 %q 含空白字符", domain)
		}
		if len(h.IPs) == 0 {
			return fmt.Errorf("域名 %s 至少需要一个 IP", domain)
		}
		ips := make([]string, 0, len(h.IPs))
		for _, raw := range h.IPs {
			ip := strings.TrimSpace(raw)
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("域名 %s 的 IP 不合法: %s", domain, raw)
			}
			ips = append(ips, ip)
		}
		sb.WriteString(domain + " " + strings.Join(ips, " ") + "\n")
	}
	path := m.HostsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, []byte(sb.String()))
}

// --- 数据库更新 ---

// defaultLogPath 默认日志文件(绝对路径,避免相对目录歧义)。
func (m *Manager) defaultLogPath() string { return filepath.Join(m.dir, logFileName) }

// LogFile 解析配置中的日志文件路径,相对路径按 mosdns 工作目录展开。
func (m *Manager) LogFile() string {
	if b, err := m.ReadConfig(); err == nil {
		var raw rawConfig
		if yaml.Unmarshal(b, &raw) == nil {
			if p := strings.TrimSpace(raw.Log.File); p != "" {
				if !filepath.IsAbs(p) {
					p = filepath.Join(m.dir, p)
				}
				return p
			}
		}
	}
	return m.defaultLogPath()
}

// APIAddr 解析配置中的 api.http 地址,缺省 127.0.0.1:9091。
func (m *Manager) APIAddr() string {
	if b, err := m.ReadConfig(); err == nil {
		var raw rawConfig
		if yaml.Unmarshal(b, &raw) == nil {
			if a := strings.TrimSpace(raw.API.HTTP); a != "" {
				return a
			}
		}
	}
	return fmt.Sprintf("127.0.0.1:%d", DefaultAPIPort)
}

// CacheTag 返回配置中 cache 插件的 tag;无缓存插件返回空。
func (m *Manager) CacheTag() string {
	b, err := m.ReadConfig()
	if err != nil {
		return ""
	}
	var raw rawConfig
	if yaml.Unmarshal(b, &raw) != nil {
		return ""
	}
	for _, p := range raw.Plugins {
		if p.Type == "cache" {
			return p.Tag
		}
	}
	return ""
}

// GeoList 数据库文件状态。
func (m *Manager) GeoList() []GeoItem {
	out := make([]GeoItem, 0, len(geoSources))
	for _, src := range geoSources {
		item := GeoItem{Name: src.Name, Label: src.Label, URL: src.URL}
		if st, err := os.Stat(filepath.Join(m.dir, src.Name)); err == nil {
			item.Size = st.Size()
			item.UpdatedAt = st.ModTime().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	return out
}

// UpdateGeodata 下载全部数据文件(逐文件原子落盘),返回每项结果。
func (m *Manager) UpdateGeodata(ctx context.Context) []GeoResult {
	if err := m.ensureDir(); err != nil {
		return []GeoResult{{Name: "*", Error: err.Error()}}
	}
	s := Settings{}
	if st, err := m.ReadSettings(); err == nil {
		s = st
	}
	results := make([]GeoResult, 0, len(geoSources))
	for _, src := range geoSources {
		res := GeoResult{Name: src.Name, Label: src.Label}
		data, err := m.fetch(ctx, s.GeoProxy+src.URL)
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		if err := atomicWrite(filepath.Join(m.dir, src.Name), data); err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.Size = int64(len(data))
		results = append(results, res)
	}
	return results
}

// UpdateAdSources 下载广告规则来源到 rule/adlist/。
func (m *Manager) UpdateAdSources(ctx context.Context) []GeoResult {
	s, err := m.ReadSettings()
	if err != nil {
		return []GeoResult{{Name: "*", Error: err.Error()}}
	}
	return m.downloadAdSources(ctx, s)
}

func (m *Manager) downloadAdSources(ctx context.Context, s Settings) []GeoResult {
	if err := m.ensureDir(); err != nil {
		return []GeoResult{{Name: "*", Error: err.Error()}}
	}
	results := make([]GeoResult, 0, len(s.AdSources))
	for _, raw := range s.AdSources {
		src := strings.TrimSpace(raw)
		if src == "" {
			continue
		}
		res := GeoResult{Name: src}
		url := src
		if strings.HasPrefix(src, "file://") {
			local := strings.TrimPrefix(src, "file://")
			b, err := os.ReadFile(local)
			if err != nil {
				res.Error = err.Error()
				results = append(results, res)
				continue
			}
			if err := atomicWrite(filepath.Join(m.dir, adlistDirName, filepath.Base(local)), b); err != nil {
				res.Error = err.Error()
				results = append(results, res)
				continue
			}
			res.Size = int64(len(b))
			results = append(results, res)
			continue
		}
		data, err := m.fetch(ctx, url)
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		name := filepath.Base(strings.SplitN(url, "?", 2)[0])
		if name == "" || name == "." || name == "/" {
			name = "adlist.txt"
		}
		if !strings.HasSuffix(name, ".txt") {
			name += ".txt"
		}
		if err := atomicWrite(filepath.Join(m.dir, adlistDirName, name), data); err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.Size = int64(len(data))
		results = append(results, res)
	}
	return results
}

// fetch 下载 URL(带 5 分钟超时)。
func (m *Manager) fetch(ctx context.Context, url string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// --- 日志 ---

// ReadLog 读取日志尾部(最多 maxBytes,至少保留整行)。
func (m *Manager) ReadLog(maxBytes int64) (string, error) {
	return tailFile(m.LogFile(), maxBytes)
}

// ClearLog 清空日志文件(不存在则忽略)。
func (m *Manager) ClearLog() error {
	err := os.Truncate(m.LogFile(), 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// tailFile 读文件尾部。
func tailFile(path string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 256 << 10
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	offset := int64(0)
	if st.Size() > maxBytes {
		offset = st.Size() - maxBytes
	}
	buf := make([]byte, st.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text := string(buf)
	if offset > 0 {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return text, nil
}

// --- 缓存刷新 ---

// FlushCache 经 mosdns 插件 API 清空缓存。
func (m *Manager) FlushCache(ctx context.Context) error {
	tag := m.CacheTag()
	if tag == "" {
		return ErrNoCache
	}
	url := fmt.Sprintf("http://%s/plugins/%s/flush", m.APIAddr(), tag)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("mosdns API 不可达(%s): %w", m.APIAddr(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("刷新缓存失败(%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// 语义化错误。
var (
	// ErrNotConfigured 配置尚未生成。
	ErrNotConfigured = errors.New("mosdns 配置尚未生成")
	// ErrNoCache 配置中未启用缓存插件。
	ErrNoCache = errors.New("未启用缓存插件，无法刷新缓存")
	// ErrBadRule 未知规则列表名。
	ErrBadRule = errors.New("未知的规则列表")
)
