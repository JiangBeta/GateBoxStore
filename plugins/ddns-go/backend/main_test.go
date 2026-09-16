package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildEntriesAndConfig(t *testing.T) {
	creds := []credential{
		{ID: "cf1", Provider: "cloudflare", Fields: map[string]string{"token": "tok-cf"},
			DDNS: &ddnsMapping{Provider: "cloudflare", SecretField: "token"}},
		{ID: "ali1", Provider: "aliyun", Fields: map[string]string{"accessKeyId": "ak", "accessKeySecret": "sk"},
			DDNS: &ddnsMapping{Provider: "alidns", IDField: "accessKeyId", SecretField: "accessKeySecret"}},
		{ID: "bad", Provider: "dnspod", Fields: map[string]string{"id": "i"},
			DDNS: &ddnsMapping{Provider: "dnspod", IDField: "id", SecretField: "token"}},
	}
	doms := []projectedDomain{
		{Host: "a.neob.cn", RootDomain: "neob.cn", CredentialID: "cf1"},
		{Host: "b.neob.cn", RootDomain: "neob.cn", CredentialID: "cf1"},
		{Host: "a.neob.cn", RootDomain: "neob.cn", CredentialID: "cf1"}, // 去重
		{Host: "x.ali.cn", RootDomain: "ali.cn", CredentialID: "ali1"},
		{Host: "no-cred.cn", RootDomain: "nobind.cn"},               // 无凭证 → 跳过
		{Host: "bad.cn", RootDomain: "bad.cn", CredentialID: "bad"}, // 凭证残缺 → 跳过
	}
	entries := buildEntries(doms, creds)
	if len(entries) != 2 {
		t.Fatalf("条目数 = %d, want 2: %+v", len(entries), entries)
	}

	data, err := buildConfig(entries)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("落盘 YAML 无法解析: %v", err)
	}
	if len(cfg.DnsConf) != 2 {
		t.Fatalf("dnsconf 数 = %d, want 2", len(cfg.DnsConf))
	}
	for _, d := range cfg.DnsConf {
		if d.DNS.Name == "aliyun" {
			t.Errorf("aliyun 应映射为 ddns-go 的 alidns，得到 %q", d.DNS.Name)
		}
	}
	if !strings.Contains(string(data), "a.neob.cn") {
		t.Error("配置应含域名 a.neob.cn")
	}
}

func TestBuildEntriesEmpty(t *testing.T) {
	entries := buildEntries(nil, nil)
	if entries == nil {
		t.Fatal("空输入应返回空切片而非 nil")
	}
	if len(entries) != 0 {
		t.Fatalf("空输入 returns %d", len(entries))
	}
}
