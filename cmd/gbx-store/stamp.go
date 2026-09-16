package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// runStamp 把 dist 目录中构建好的制品回填到 manifest.yaml 的 artifacts[].url/sha256/size。
//
// 用 yaml.Node 读写以**保留注释与顺序**；按 role 约定文件名：
//
//	sidecar → <id>-sidecar-<os>-<arch>.tar.gz
//	ui      → <id>-ui.tar.gz
//	assets  → <id>-assets.tar.gz
//	binary  → <id>-<os>-<arch>.tar.gz
//
// 用法：gbx-store stamp -plugin plugins/mosdns -dist dist/mosdns [-base-url https://.../releases/download/mosdns/v0.1.0]
func runStamp(args []string) error {
	fs := flag.NewFlagSet("stamp", flag.ContinueOnError)
	pluginDir := fs.String("plugin", "", "插件目录（含 manifest.yaml，必填）")
	distDir := fs.String("dist", "", "制品目录（含 *.tar.gz，必填）")
	baseURL := fs.String("base-url", "", "制品 URL 前缀（可选；给出则同时回填 url）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pluginDir == "" || *distDir == "" {
		return fmt.Errorf("-plugin 与 -dist 必填")
	}
	manPath := filepath.Join(*pluginDir, "manifest.yaml")
	raw, err := os.ReadFile(manPath)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("manifest 为空: %s", manPath)
	}
	root := doc.Content[0]
	id := scalar(root, "id")
	if id == "" {
		return fmt.Errorf("manifest 缺少 id: %s", manPath)
	}
	arts := mapGet(root, "artifacts")
	if arts == nil || arts.Kind != yaml.SequenceNode {
		return fmt.Errorf("manifest 无 artifacts 列表: %s", manPath)
	}

	stamped := 0
	for _, item := range arts.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		role := scalar(item, "role")
		if role == "" {
			role = "binary"
		}
		osName := scalar(item, "os")
		if osName == "" {
			osName = "linux"
		}
		archName := scalar(item, "arch")
		fname := artifactFilename(id, role, osName, archName)
		p := filepath.Join(*distDir, fname)
		data, err := os.ReadFile(p)
		if err != nil {
			continue // 未构建该制品（如上游 binary）→ 跳过
		}
		sum := sha256.Sum256(data)
		if *baseURL != "" {
			setScalar(item, "url", strings.TrimRight(*baseURL, "/")+"/"+fname)
		}
		setScalar(item, "sha256", hex.EncodeToString(sum[:]))
		setScalar(item, "size", fmt.Sprintf("%d", len(data)))
		stamped++
		fmt.Printf("stamp %s %s sha256=%s size=%d\n", id, fname, hex.EncodeToString(sum[:])[:12], len(data))
	}
	if stamped == 0 {
		return fmt.Errorf("dist 中未找到任何可回填制品（检查文件名约定）: %s", *distDir)
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(manPath, out, 0o644)
}

// artifactFilename 按 role 约定生成制品文件名。
func artifactFilename(id, role, osName, archName string) string {
	switch role {
	case "sidecar":
		return fmt.Sprintf("%s-sidecar-%s-%s.tar.gz", id, osName, archName)
	case "ui":
		return fmt.Sprintf("%s-ui.tar.gz", id)
	case "assets":
		return fmt.Sprintf("%s-assets.tar.gz", id)
	default:
		return fmt.Sprintf("%s-%s-%s.tar.gz", id, osName, archName)
	}
}

// mapGet 返回映射节点中 key 对应的值节点。
func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// scalar 读取映射节点中 key 的标量值。
func scalar(n *yaml.Node, key string) string {
	if v := mapGet(n, key); v != nil {
		return v.Value
	}
	return ""
}

// setScalar 设置映射节点中 key 的标量值（不存在则追加）。
func setScalar(n *yaml.Node, key, val string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1].Value = val
			n.Content[i+1].Style = 0
			return
		}
	}
	n.Content = append(n.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: val},
	)
}
