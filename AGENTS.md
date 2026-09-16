# AGENTS.md — GateBoxStore 开发指南

> 完整说明见 `README.md`。本文件只列 agent 容易踩坑的高信号信息。

## 语言

沟通、思考、代码注释**一律中文**。文档、`manifest.yaml` 的 `summary` 亦然（`id` / `field name` 用英文）。

## 本仓库是什么

GateBox 的**官方插件仓库 + 静态索引**。两个核心产物：

1. `plugins/<id>/` 的插件源码 → CI 构建为签名 tar.gz 发布到 GitHub Releases；
2. `index.json`（`catalog.v1`）→ 静态托管，GateBox 客户端据此在线安装。

**这不是 GateBox 主程序**：没有 HTTP 服务端、没有数据库、没有前端应用。本仓库只产出「数据 + 制品 + 索引」。

## 快速命令

```bash
go run ./cmd/gbx-store index          # 扫描 plugins/*/manifest.yaml 生成 index.json
go run ./cmd/gbx-store index -check    # 仅校验 schema，不写文件
go run ./cmd/gbx-store pack -dir plugins/<id> -out dist/   # 打包插件目录为 tar.gz 并计算 sha256
scripts/build-plugin.sh <id>           # 构建插件的 sidecar/ui/assets 制品 → dist/<id>/
go run ./cmd/gbx-store stamp -plugin plugins/<id> -dist dist/<id> -base-url <releaseURL>
                                       # 回填 artifacts[].url/sha256/size（保留注释，不手改）
go run ./cmd/gbx-store keygen -out keys          # 生成发布者 Ed25519 密钥对
go run ./cmd/gbx-store sign -in index.json       # 生成 index.json.sig
go run ./cmd/gbx-store verify -in index.json -pub <base64公钥>   # 校验索引签名
go run ./cmd/gbx-store variant-key -component caddy -version 2.10.0 -os linux -arch amd64 -features a,b

go build ./...                         # 编译工具
go vet ./...                           # 静态检查
gofmt -l ./cmd                         # 有输出即不合规
```

工具链：Go 1.26（`mise` 管理，见 `mise.toml`）。环境**没有 python3 / yq**，脚本一律用 Go 或 `jq` + `sha256sum` + `tar`。

## 致命顺序

1. **改 `schema/` 前必须先改主仓库**：`schema/` 是从 `JiangBeta/gatebox` vendor 的契约副本。修改流程 = 先在主仓库改 schema 并评估 `extensionApi` 版本 → 再同步到本仓库 → CI 校验两仓一致。**禁止只改本仓库**。
2. **`index.json` 是生成物**，不要手写。改名单/版本请改 `plugins/*/manifest.yaml` 或 `variants.yaml`，再 `go run ./cmd/gbx-store index`。
3. **改 `extensionApi` = 破坏性变更**：任何对贡献点字段的删除/语义变更都要在主仓库写 ADR，并升 major。
4. **制品 URL 必须带 `sha256`**：GateBox 安装时校验；缺失即视为不安全。

## 架构要点

- **统一契约**：`kind ∈ {caddy-module, process, config-only}`，行为差异全部由 `contributions` 与 `artifacts.role` 导出，**不新增 kind、不新增引擎分支**（ADR-037）。
- **三类插件**：核心插件扩展（`caddy-module`，声明中性 `feature`，走配方变体）/ 独立进程（`process`，`sidecar` + `ui`）/ 纯声明式（`config-only`，如 `dns-provider`）。
- **配方变体**（ADR-038）：`caddy-module` **不携带整包二进制**，只声明特征（如 `github.com/mholt/caddy-l4`）；本仓库 CI 用 `xcaddy` 按特征并集构建变体，写入 `variants.yaml` 与 `index.json` 的 `variants[]`。
- **三个不变式**（主仓库 ADR-036）：核心无插件身份；核心→插件只经投影 API；插件→核心只经贡献声明。
- **权限**：`permissions.{filesystem,api,network}` 由 GateBox 以 **plugin token + scope** 实际强制，声明需最小化。

## 插件目录约定

```
plugins/<id>/
├── manifest.yaml     # 必需；符合 schema/manifest.v2.schema.json
├── backend/          # kind:process 的 sidecar 源码（Go；编译产物为 role:sidecar）
├── ui/               # L1 iframe 前端源码（构建产物为 role:ui，format:iframe）
├── assets/           # 静态资源（如 acme dnsapi 脚本，role:assets）
└── README.md         # 插件说明（面向用户）
```

- 插件 `id` 规则：`^[a-z][a-z0-9-]{1,31}$`，一经发布**不可改名**（客户端按 id 识别）。
- 插件 `version` 遵循 semver；发布后同版本不可覆盖。

## 安全

- 安装第三方制品 = 在用户设备上执行代码。**禁止**在仓库内提交任何密钥、token、私钥；凭证由用户在 GateBox 内录入并加密存储。
- 提交前自查：`grep -rn "BEGIN.*PRIVATE KEY\|password\|secret\|token" plugins/` 不应有真实值。
- `community` 级插件不得要求无理由的高权限（如 `filesystem.write: /`）。

## 目录约定

```
cmd/gbx-store/       # 维护工具（index / pack）
plugins/<id>/        # 插件源码
schema/              # vendor 的契约 schema（权威在主仓库）
docs/                # 插件作者文档
variants.yaml        # 配方变体清单
index.json           # 生成的索引（勿手改）
.github/workflows/   # CI
```

## 文档

- `README.md` · `docs/plugin-authoring.md` · `docs/catalog.md` · `docs/extension-api.md` · `docs/plugin-ui.md`
- 宿主设计（主仓库）：ADR-027（配方边界）· ADR-036（扩展平台）· ADR-037（分离）· ADR-038（配方变体）· ADR-039（运行时契约）
