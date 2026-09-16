package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
)

// runVerify 校验 detached Ed25519 签名（与 GateBox 客户端同一算法）。
//
// 用法：gbx-store verify -in index.json -sig index.json.sig -pub <base64公钥>
func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	in := fs.String("in", "index.json", "被签名文件")
	sigFile := fs.String("sig", "", "签名文件（默认 <in>.sig）")
	pubB64 := fs.String("pub", "", "Ed25519 公钥（base64，必填）")
	pubFile := fs.String("pub-file", "", "从文件读取公钥（可选，优先于 -pub）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sigFile == "" {
		*sigFile = *in + ".sig"
	}
	pub := strings.TrimSpace(*pubB64)
	if *pubFile != "" {
		b, err := os.ReadFile(*pubFile)
		if err != nil {
			return err
		}
		pub = strings.TrimSpace(string(b))
	}
	if pub == "" {
		return fmt.Errorf("-pub 或 -pub-file 必填")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sigB64, err := os.ReadFile(*sigFile)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil {
		return fmt.Errorf("签名 base64 非法: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(pub)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("公钥非法")
	}
	if !ed25519.Verify(ed25519.PublicKey(key), data, raw) {
		return fmt.Errorf("验签失败：签名与内容或公钥不匹配")
	}
	fmt.Printf("验签通过：%s 由该公钥签名\n", *in)
	return nil
}
