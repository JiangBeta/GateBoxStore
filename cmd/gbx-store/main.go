// Command gbx-store 是 GateBoxStore 的维护工具。
//
// 子命令：
//
//	index  扫描 plugins/*/manifest.yaml 与 variants.yaml，生成或校验 index.json
//	pack   把单个插件目录打包为 tar.gz 并计算 sha256（供 CI 发布）
//
// 设计见 GateBox 主仓库 docs/adr/ADR-037（分离与分发）、ADR-038（配方变体）。
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const catalogSchema = "gatebox.catalog/v1"

var (
	idPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	kinds        = []string{"caddy-module", "process", "config-only"}
	capPoints    = []string{"proxy-protocols", "validator", "component-variant", "dns-provider"}
	backendPoint = []string{"renderer", "config-sync", "reconcile"}
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "index":
		err = runIndex(os.Args[2:])
	case "pack":
		err = runPack(os.Args[2:])
	case "variant-key":
		err = runVariantKey(os.Args[2:])
	case "stamp":
		err = runStamp(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		err = fmt.Errorf("未知子命令: %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法:
  gbx-store index       [-plugins dir] [-variants file] [-out file] [-extension-api n] [-check]
  gbx-store pack        -dir plugins/<id> -out dist/ [-name <id>.tar.gz]
  gbx-store variant-key -component caddy -version 2.10.0 -os linux -arch amd64 -features mod1,mod2

  index  -check   仅校验 manifest，不写文件
`)
}

// ---------- variant-key ----------

func runVariantKey(args []string) error {
	fs := flag.NewFlagSet("variant-key", flag.ContinueOnError)
	component := fs.String("component", "", "组件 id（必填）")
	version := fs.String("version", "", "组件版本（必填）")
	osName := fs.String("os", "linux", "os")
	arch := fs.String("arch", "amd64", "arch")
	features := fs.String("features", "", "逗号分隔的特征列表")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" || *version == "" {
		return fmt.Errorf("-component 与 -version 必填")
	}
	var list []string
	if strings.TrimSpace(*features) != "" {
		list = strings.Split(*features, ",")
	}
	fmt.Println(variantKey(*component, *version, *osName, *arch, list))
	return nil
}

// ---------- index ----------

type catalog struct {
	Schema       string         `json:"schema"`
	ExtensionAPI int            `json:"extensionApi"`
	GeneratedAt  string         `json:"generatedAt"`
	Publisher    map[string]any `json:"publisher,omitempty"`
	Plugins      []catalogEntry `json:"plugins"`
	Variants     []variant      `json:"variants,omitempty"`
}

type catalogEntry struct {
	ID       string           `json:"id"`
	Versions []catalogVersion `json:"versions"`
}

type catalogVersion struct {
	Version      string           `json:"version"`
	Channel      string           `json:"channel"`
	PublishedAt  string           `json:"publishedAt"`
	ExtensionAPI int              `json:"extensionApi"`
	Manifest     map[string]any   `json:"manifest"`
	Artifacts    []map[string]any `json:"artifacts,omitempty"`
	Signature    string           `json:"signature,omitempty"`
}

type variant struct {
	Key       string   `json:"key"`
	Component string   `json:"component"`
	Version   string   `json:"version"`
	Features  []string `json:"features"`
	OS        string   `json:"os"`
	Arch      string   `json:"arch"`
	URL       string   `json:"url"`
	SHA256    string   `json:"sha256,omitempty"`
	Size      int64    `json:"size,omitempty"`
}

func runIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	pluginsDir := fs.String("plugins", "plugins", "插件目录")
	variantsFile := fs.String("variants", "variants.yaml", "变体清单文件")
	out := fs.String("out", "index.json", "输出文件")
	extAPI := fs.Int("extension-api", 1, "索引整体的 extensionApi 版本")
	pubID := fs.String("publisher-id", "jiangbeta", "发布者 id")
	pubName := fs.String("publisher-name", "GateBox", "发布者名称")
	check := fs.Bool("check", false, "仅校验，不写文件")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries := []catalogEntry{}
	dirs, err := os.ReadDir(*pluginsDir)
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		manPath := filepath.Join(*pluginsDir, d.Name(), "manifest.yaml")
		if _, err := os.Stat(manPath); err != nil {
			continue
		}
		man, err := loadManifest(manPath)
		if err != nil {
			return fmt.Errorf("%s: %w", manPath, err)
		}
		if err := validateManifest(man); err != nil {
			return fmt.Errorf("%s: %w", manPath, err)
		}
		if d.Name() != str(man["id"]) {
			return fmt.Errorf("%s: 目录名 %q 与 id %q 不一致", manPath, d.Name(), str(man["id"]))
		}
		entry := catalogEntry{
			ID: str(man["id"]),
			Versions: []catalogVersion{{
				Version:      str(man["version"]),
				Channel:      str(man["channel"]),
				PublishedAt:  time.Now().UTC().Format(time.RFC3339),
				ExtensionAPI: *extAPI,
				Manifest:     man,
			}},
		}
		if arts, ok := man["artifacts"].([]any); ok {
			for _, a := range arts {
				if m, ok := a.(map[string]any); ok {
					entry.Versions[0].Artifacts = append(entry.Versions[0].Artifacts, m)
				}
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	variants, err := loadVariants(*variantsFile)
	if err != nil {
		return err
	}

	cat := catalog{
		Schema:       catalogSchema,
		ExtensionAPI: *extAPI,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Publisher:    map[string]any{"id": *pubID, "name": *pubName},
		Plugins:      entries,
		Variants:     variants,
	}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if *check {
		fmt.Printf("校验通过：%d 个插件，%d 个变体\n", len(entries), len(variants))
		return nil
	}
	return os.WriteFile(*out, data, 0o644)
}

func loadManifest(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func validateManifest(m map[string]any) error {
	required := []string{"apiVersion", "kind", "id", "name", "version"}
	for _, k := range required {
		if strings.TrimSpace(str(m[k])) == "" {
			return fmt.Errorf("缺少必填字段 %q", k)
		}
	}
	if str(m["apiVersion"]) != "gatebox/v2" {
		return fmt.Errorf("apiVersion 必须为 gatebox/v2，实际 %q", str(m["apiVersion"]))
	}
	if !contains(kinds, str(m["kind"])) {
		return fmt.Errorf("kind %q 非法（%v）", str(m["kind"]), kinds)
	}
	if !idPattern.MatchString(str(m["id"])) {
		return fmt.Errorf("id %q 不符合 ^[a-z][a-z0-9-]{1,31}$", str(m["id"]))
	}
	if cont, ok := m["contributions"].(map[string]any); ok {
		if caps, ok := cont["capabilities"].([]any); ok {
			for _, c := range caps {
				cm, _ := c.(map[string]any)
				if p := str(cm["point"]); !contains(capPoints, p) {
					return fmt.Errorf("capabilities.point %q 非法（%v）", p, capPoints)
				}
			}
		}
		if backs, ok := cont["backend"].([]any); ok {
			for _, c := range backs {
				cm, _ := c.(map[string]any)
				if p := str(cm["point"]); !contains(backendPoint, p) {
					return fmt.Errorf("backend.point %q 非法（%v）", p, backendPoint)
				}
			}
		}
	}
	if arts, ok := m["artifacts"].([]any); ok {
		for i, a := range arts {
			am, _ := a.(map[string]any)
			if str(am["role"]) == "" || str(am["url"]) == "" {
				return fmt.Errorf("artifacts[%d] 缺少 role 或 url", i)
			}
		}
	}
	return nil
}

func loadVariants(path string) ([]variant, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Variants []variant `yaml:"variants"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	for i := range doc.Variants {
		v := &doc.Variants[i]
		if v.Key == "" {
			v.Key = variantKey(v.Component, v.Version, v.OS, v.Arch, v.Features)
		}
		if len(v.Features) == 0 {
			v.Features = []string{}
		}
	}
	return doc.Variants, nil
}

// variantKey 计算配方变体键。**算法必须与 GateBox 内核一致**（ADR-038 §2 / docs/catalog.md §3）。
func variantKey(component, version, osName, arch string, features []string) string {
	seen := map[string]bool{}
	fs := make([]string, 0, len(features))
	for _, f := range features {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		fs = append(fs, f)
	}
	sort.Strings(fs)

	var b strings.Builder
	b.WriteString("component=" + strings.TrimSpace(component) + "\n")
	b.WriteString("version=" + strings.TrimSpace(version) + "\n")
	b.WriteString("os=" + strings.TrimSpace(osName) + "\n")
	b.WriteString("arch=" + strings.TrimSpace(arch) + "\n")
	for _, f := range fs {
		b.WriteString("feature=" + f + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// ---------- pack ----------

func runPack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	dir := fs.String("dir", "", "要打包的插件目录（必填）")
	out := fs.String("out", "dist", "输出目录")
	name := fs.String("name", "", "输出文件名（默认 <id>.tar.gz）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir 必填")
	}
	base := filepath.Base(filepath.Clean(*dir))
	fname := *name
	if fname == "" {
		fname = base + ".tar.gz"
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(*out, fname)
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(f, h))
	tw := tar.NewWriter(gz)

	err = filepath.WalkDir(*dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(*dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	info, err := os.Stat(dest)
	if err != nil {
		return err
	}
	fmt.Printf("%s  sha256=%s  size=%d\n", dest, hex.EncodeToString(h.Sum(nil)), info.Size())
	return nil
}

// ---------- helpers ----------

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
