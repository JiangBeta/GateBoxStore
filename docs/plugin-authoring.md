# 插件作者指南

> 面向为 GateBox 编写插件的开发者。先读 [扩展 API 参考](extension-api.md) 与 [插件 UI 指南](plugin-ui.md)。
> 设计背景：ADR-037（主程序与插件分离）、ADR-038（配方变体）、ADR-039（运行时契约）。

## 1. 心智模型

插件是**数据**：一个 `manifest.yaml` + 通用引擎 + 你贡献的制品。GateBox 内核不认识任何具体插件，只认识**贡献点**（capability / renderer / config-sync / reconcile / ui / data）与**制品角色**（`binary` / `sidecar` / `ui` / `assets`）。

三个不变式（必须遵守）：

- **核心无插件身份**：你不能要求内核为你的插件加特判；
- **核心 → 插件**：只经投影 API（只读、scoped、带 revision）；
- **插件 → 核心**：只经贡献声明 + 契约调用；不读核心数据库、不触碰核心前端 store/DOM。

## 2. 目录结构

```
plugins/<id>/
├── manifest.yaml     # 必需
├── backend/          # kind:process 的 sidecar 源码
├── ui/               # L1 iframe 前端源码
├── assets/           # 静态资源（如 acme dnsapi 脚本）
└── README.md         # 面向用户的说明
```

## 3. 选哪种 kind

| 需求 | kind | 关键 |
|---|---|---|
| 给核心组件加**编译期模块**（Caddy 模块等） | `caddy-module` | 声明中性 `feature`（`component-variant` 贡献）；不打包整包二进制，变体由本仓库构建 |
| 有**自己的后端进程与页面** | `process` | 提供 `sidecar` + `ui` 制品；sidecar 只监听本地 |
| 只做**声明式扩展**，无进程无页面 | `config-only` | 如 `dns-provider`、`renderer` 模板 |

## 4. 示例：核心插件扩展（caddy-l4）

`plugins/caddy-l4/manifest.yaml`：

```yaml
apiVersion: gatebox/v2
kind: caddy-module
id: caddy-l4
name: Caddy L4
version: 0.1.0
summary: 为 Caddy 增加 TCP/UDP（L4）代理能力
channel: official
requires:
  gatebox: ">=0.4.0"
  extensionApi: ">=1"
  components: [caddy]
contributions:
  capabilities:
    - point: component-variant
      data: { component: caddy, feature: "github.com/mholt/caddy-l4" }
    - point: proxy-protocols
      data: { class: non-http, label: TCP/UDP, networks: [tcp, udp], requiresPrimaryDomain: false }
  backend:
    - point: renderer
      for: non-http
      scope: global
      impl: { type: template, template: "..." }
  ui:
    slots:
      - { slot: port-form.protocol-options, from: proxy-protocols }
permissions:
  - { api: [gateway:read] }
```

要点：**不提供 `role:binary` 制品**。安装 = 把 `github.com/mholt/caddy-l4` 加入特征并集，由 GateBoxStore CI 构建含该特征的 Caddy 变体（ADR-038）。两个此类插件（如 l4 + coraza）会合并为一个变体二进制，不再互相覆盖。

## 5. 示例：独立进程插件（mosdns 类）

```yaml
apiVersion: gatebox/v2
kind: process
id: mosdns
name: MosDNS
version: 0.1.0
summary: 内网 DNS 解析
channel: official
requires: { gatebox: ">=0.4.0", extensionApi: ">=1", os: [linux] }
artifacts:
  - role: sidecar
    os: linux
    arch: amd64
    url: https://github.com/JiangBeta/GateBoxStore/releases/download/mosdns/v0.1.0/mosdns-sidecar-linux-amd64.tar.gz
    sha256: "..."
    install: { to: tools/mosdns/, mode: "0755" }
  - role: ui
    format: iframe
    url: https://github.com/JiangBeta/GateBoxStore/releases/download/mosdns/v0.1.0/mosdns-ui.tar.gz
    sha256: "..."
contributions:
  ui:
    nav: [{ path: /plugins/mosdns, label: MosDNS }]
permissions:
  - { filesystem: { write: [tools/mosdns] } }
  - { api: [domain:read] }
```

- `sidecar` 由 GateBox 进程托管器拉起，**只监听 `127.0.0.1` 或 unix socket**；内核反代 `/api/v1/plugins/<id>/*`。
- `ui`（`format: iframe`）安装到 `$DATA_DIR/tools/<id>/ui`，内核托管于 `/plugins/<id>/`。
- 前端只通过同源 API 与 [postMessage 桥](plugin-ui.md) 与宿主交互。

## 6. 示例：纯声明式扩展（dns-provider）

```yaml
apiVersion: gatebox/v2
kind: config-only
id: dns-cloudflare
name: Cloudflare DNS Provider
version: 0.1.0
summary: Cloudflare DNS 凭证供应商（acme DNS-01 + ddns-go）
channel: official
requires: { extensionApi: ">=1", components: [acme] }
contributions:
  capabilities:
    - point: dns-provider
      data:
        id: cloudflare
        label: Cloudflare
        acmeHook: dns_cf
        fields:
          - { name: token, label: API Token, type: password, required: true, secret: true }
        envMap: { token: CF_Token }
        ddnsProvider: cloudflare
artifacts:
  - role: assets
    url: https://github.com/JiangBeta/GateBoxStore/releases/download/dns-cloudflare/v0.1.0/dnsapi.tar.gz
    sha256: "..."
    install: { to: tools/acme/dnsapi/ }
```

内核据 `fields` 动态渲染凭证表单（`GET /api/v1/credentials/providers`），`acmeHook`/`envMap` 驱动 acme.sh，无需改内核。

## 7. 制品与角色

| role | 用途 | 落点 |
|---|---|---|
| `binary` | 插件自有整包二进制（如互斥的组件整包覆盖） | 见 `install.to` |
| `sidecar` | 后端逻辑进程 | `tools/<id>/` |
| `ui` | 前端页面（`format: iframe` 或 `esm`） | `tools/<id>/ui` |
| `assets` | 静态资源 | 见 `install.to` |

- 每个制品**必须**有 `sha256`；按 `os`/`arch` 分平台。
- URL 指向本仓库的 GitHub Releases。

## 8. 权限

```yaml
permissions:
  - { filesystem: { write: [tools/mosdns] } }   # 路径白名单，内核强制
  - { api: [domain:read, gateway:read] }        # plugin token scope，内核校验
  - { network: ["tcp:53"] }                     # 声明并在安装确认页展示
```

**最小权限原则**：只申请真正需要的。高权限（如写 `tools/caddy`）会触发用户显式确认。

## 9. 校验与本地预览

```bash
go run ./cmd/gbx-store index -check     # 校验 manifest 符合 schema（不写文件）
go run ./cmd/gbx-store index            # 生成 index.json 预览
go run ./cmd/gbx-store pack -dir plugins/<id> -out dist/
```

PR 合并后由 CI 构建制品并更新索引。

## 10. 版本与兼容

- `version`：semver；发布后不可覆盖同版本。
- `requires.gatebox`：宿主版本约束；不满足时 GateBox 拒绝加载并提示。
- `requires.extensionApi`：契约版本；破坏性升级需在主仓库写 ADR。
