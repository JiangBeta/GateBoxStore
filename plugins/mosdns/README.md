# MosDNS

内网 DNS 解析与分流（独立进程插件）。

- **kind**：`process`
- **构成**：`sidecar`（管理后端，Go）+ `ui`（管理页，Vue，L1 iframe）+ `binary`（mosdns 本体）
- **进程模型**：GateBox 只托管 sidecar；mosdns 本体由 sidecar 代管（ADR-037 决策 (a)）

## 目录

```
plugins/mosdns/
├── manifest.yaml
├── backend/            # Go 模块：管理 API + mosdns 进程托管
│   ├── main.go         # HTTP 侧车（端口/令牌来自环境变量）
│   └── mosdns/         # 配置生成、规则、hosts、geo、日志
└── ui/                 # Vue 管理页（构建产物作为 role:ui 制品）
```

## 构建

```bash
# 侧车
cd backend
go test ./...
go build -o ../../../dist/mosdns-sidecar ./
tar -czf ../../../dist/mosdns-sidecar-linux-amd64.tar.gz -C ../../../dist mosdns-sidecar
sha256sum ../../../dist/mosdns-sidecar-linux-amd64.tar.gz   # 回填 manifest.artifacts[role=sidecar].sha256

# 管理页
cd ../ui
pnpm install
pnpm build
tar -czf ../../../dist/mosdns-ui.tar.gz -C dist .
sha256sum ../../../dist/mosdns-ui.tar.gz                    # 回填 manifest.artifacts[role=ui].sha256
```

## 运行时契约

- 侧车监听 `127.0.0.1:$GATEBOX_PLUGIN_PORT`，请求须带 `X-Plugin-Token: $GATEBOX_PLUGIN_TOKEN`。
- 内核反代 `/api/v1/plugins/mosdns/*` → 侧车根路径；管理页由内核托管于 `/plugins/mosdns/`。
- mosdns 二进制位于 `<DATA_DIR>/tools/mosdns/mosdns`，运行目录同目录（`config.yaml` 等）。

## 端点

`GET /status`、`POST /start|/stop|/restart`、`GET|PUT /settings`、`GET|PUT /config`、`GET|PUT /hosts`、`GET /rules`、`GET|PUT /rules/{name}`、`GET /geodata`、`POST /geodata/update`、`POST /adblock/update`、`GET|DELETE /logs`、`GET /logs/stream`（WS）、`POST /flush`。

## 权限

- `filesystem.write: [tools/mosdns]`
- `api: [domain:read]`
