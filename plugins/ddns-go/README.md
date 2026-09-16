# ddns-go

动态 DNS：把网关服务的二级域名上报到 DNS 服务商，解析到本机公网 IP。

- **kind**：`process`（消费型 / reconcile）
- **构成**：`sidecar`（投影消费 + 配置渲染 + 进程代管）+ `binary`（ddns-go 本体）
- **无独立页面**：纯后台收敛，状态经 `/status` 查询（核心可聚合展示）。

## 工作方式

```
GateBox 控制面
  ├─ domains 投影      /api/v1/extensions/me/projection/domains      (scope domain:read)
  └─ credentials 投影  /api/v1/extensions/me/projection/credentials  (scope credentials:read)
        ▲  X-Plugin-Token
        │  长轮询 ?since=<revision>&wait=30（内容变更即返回）
   ddns-go sidecar ──渲染──▶ <DATA_DIR>/tools/ddnsgo/.ddns_go_config.yaml
        └─ 代管 ddns-go 进程（ddns-go 周期性重读配置，无需重启）
```

- 供应商映射（ddns-go 名称、ID/Secret 字段）来自 credentials 投影随附的 `ddns` 元数据，插件**不内置供应商知识**。
- 凭证密钥仅经**本机反代**投递，不进 `domains` 投影（ADR-036 §6）。

## 构建

```bash
cd backend
go test ./...
go build -o ../../../dist/ddns-go-sidecar ./
tar -czf ../../../dist/ddns-go-sidecar-linux-amd64.tar.gz -C ../../../dist ddns-go-sidecar
sha256sum ../../../dist/ddns-go-sidecar-linux-amd64.tar.gz   # 回填 manifest sidecar.sha256
```

## 端点

`GET /status`、`POST /sync`（强制收敛）、`POST /start|/stop|/restart`。

## 权限

- `filesystem.write: [tools/ddnsgo]`
- `api: [domain:read, credentials:read]`
