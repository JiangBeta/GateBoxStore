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

// runVariantStamp 把构建好的变体制品登记进 variants.yaml（key/url/sha256/size）。
//
// 用法：gbx-store variant-stamp -component caddy -version 2.11.4 -features "a,b" \
//
//	-file dist/variants/caddy-....tar.gz -base-url https://.../releases/download/variants/<tag>
func runVariantStamp(args []string) error {
	fs := flag.NewFlagSet("variant-stamp", flag.ContinueOnError)
	component := fs.String("component", "", "组件（必填）")
	version := fs.String("version", "", "组件版本（必填）")
	features := fs.String("features", "", "逗号分隔特征")
	file := fs.String("file", "", "变体制品文件（必填）")
	baseURL := fs.String("base-url", "", "制品 URL 前缀（必填）")
	variants := fs.String("variants", "variants.yaml", "variants.yaml 路径")
	osName := fs.String("os", "linux", "os")
	arch := fs.String("arch", "amd64", "arch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" || *version == "" || *file == "" || *baseURL == "" {
		return fmt.Errorf("-component/-version/-file/-base-url 必填")
	}
	var list []string
	if strings.TrimSpace(*features) != "" {
		list = strings.Split(*features, ",")
	}
	key := variantKey(*component, *version, *osName, *arch, list)

	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	entry := map[string]any{
		"key": key, "component": *component, "version": *version,
		"features": list, "os": *osName, "arch": *arch,
		"url":    strings.TrimRight(*baseURL, "/") + "/" + filepath.Base(*file),
		"sha256": hex.EncodeToString(sum[:]), "size": len(data),
	}

	// 保留注释：用 yaml.Node 读写。
	raw, err := os.ReadFile(*variants)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("variants.yaml 为空")
	}
	root := doc.Content[0]
	seq := mapGet(root, "variants")
	if seq == nil {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "variants"},
			&yaml.Node{Kind: yaml.SequenceNode})
		seq = mapGet(root, "variants")
	}
	var item yaml.Node
	if err := item.Encode(entry); err != nil {
		return err
	}
	// 去重：同 key 覆盖。
	replaced := false
	for i, n := range seq.Content {
		if n.Kind == yaml.MappingNode && scalar(n, "key") == key {
			seq.Content[i] = &item
			replaced = true
			break
		}
	}
	if !replaced {
		seq.Content = append(seq.Content, &item)
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*variants, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("variant-stamp %s key=%s sha=%s size=%d\n", *component, key, hex.EncodeToString(sum[:])[:12], len(data))
	return nil
}
