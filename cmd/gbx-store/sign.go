package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runKeygen 生成发布者 Ed25519 密钥对（私钥保密、公钥配到 GateBox catalog_pubkey）。
func runKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "keys", "输出目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	privB64 := base64.StdEncoding.EncodeToString(priv)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := os.WriteFile(filepath.Join(*out, "catalog.key"), []byte(privB64+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "catalog.pub"), []byte(pubB64+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("私钥: %s（保密；配到 CI Secret GBX_STORE_KEY）\n", filepath.Join(*out, "catalog.key"))
	fmt.Printf("公钥: %s（配到 GateBox 的 catalog_pubkey）\n", pubB64)
	return nil
}

// runSign 对 index.json 生成 detached 签名 index.json.sig（GateBox 客户端据此验签）。
func runSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	in := fs.String("in", "index.json", "待签名文件")
	keyFile := fs.String("key", "catalog.key", "私钥文件（base64）")
	out := fs.String("out", "", "签名输出（默认 <in>.sig）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dest := *out
	if dest == "" {
		dest = *in + ".sig"
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	keyB64, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	priv, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyB64)))
	if err != nil {
		return fmt.Errorf("私钥 base64 非法: %w", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("私钥长度非法: %d", len(priv))
	}
	sig := ed25519.Sign(ed25519.PrivateKey(priv), data)
	if err := os.WriteFile(dest, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("已签名: %s\n", dest)
	return nil
}
