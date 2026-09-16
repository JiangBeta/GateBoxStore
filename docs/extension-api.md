# 扩展 API 参考

> 契约版本：`extensionApi: 1`。权威 schema：主仓库 `plugins/schema/manifest.v2.schema.json` 与 `registry/schema/catalog.v1.schema.json`。
> 设计：ADR-036（扩展平台）、ADR-037（分离与版本化）、ADR-038（配方变体）、ADR-039（运行时契约）。

## 1. Manifest 顶层字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `apiVersion` | ✓ | 固定 `gatebox/v2` |
| `kind` | ✓ | `caddy-module` \| `process` \| `config-only` |
| `id` | ✓ | `^[a-z][a-z0-9-]{1,31}$`，发布后不可改 |
| `name` / `summary` / `tags` | | 展示信息（中文） |
| `version` | ✓ | semver |
| `channel` | | `official` \| `stable` \| `beta` \| `custom` \| `system` |
| `publisher` | | `{id, name, keyID}` |
| `requires` | | 见下 |
| `artifacts[]` | | 见 §2 |
| `contributions` | | 见 §3 |
| `permissions[]` | | 见 §4 |

### requires

| 字段 | 说明 |
|---|---|
| `gatebox` | 宿主版本约束（semver range），不满足则拒绝加载 |
| `extensionApi` | 契约版本约束 |
| `components` | 依赖的核心组件，如 `[caddy]` / `[acme]` |
| `os` / `arch` | 平台限定 |

## 2. 制品 artifacts

| role | 用途 | 典型 `install.to` |
|---|---|---|
| `binary` | 插件自有整包二进制（互斥场景） | `tools/<id>/` |
| `sidecar` | 后端逻辑进程（`kind:process`） | `tools/<id>/` |
| `ui` | 前端页面（`format: iframe` \| `esm`） | `tools/<id>/ui` |
| `assets` | 静态资源（dnsapi 脚本等） | 如 `tools/acme/dnsapi/` |

字段：`role`（必）、`url`（必）、`sha256`（**必，安全底线**）、`size`、`os`、`arch`、`format`、`entry`、`install{to,mode}`。

## 3. 贡献 contributions

### 3.1 capabilities（能力型）

| point | 语义 | data 字段 |
|---|---|---|
| `proxy-protocols` | 可代理的协议类别 | `class`（`http`\|`non-http`）、`label`、`protocols[]`、`networks[]`、`requiresPrimaryDomain` |
| `component-variant` | 给核心组件加编译期特征 | `component`、`feature`（中性标识，如 Go 模块路径） |
| `dns-provider` | DNS 凭证供应商 | `id`、`label`、`acmeHook`、`fields[]`、`envMap{}`、`ddns{provider,idField,secretField}` |
| `validator` | 校验器（可指定用哪个制品） | `bin`（缺省 = 当前 active 组件制品） |

`dns-provider.fields[]`：`{name, label, type(password\|text), required, secret}`。

### 3.2 backend（逻辑型）

| point | 语义 | 字段 |
|---|---|---|
| `renderer` | 中性规则 → Caddyfile 片段 | `for`（协议类别）、`scope`（`global`\|`site`）、`impl{type: template, template}` 或 `impl{type: sidecar, entry}` |
| `config-sync` | 从投影渲染插件配置并落盘 + reload | `projection`、`target`、`input`、`impl` |
| `reconcile` | 核心通知侧车，侧车拉投影自收敛 | `entry`、`projection`、`interval` |

- `template` 用 Go `text/template`；`renderer` 的数据为 `struct{ Rules []ProxyRule }`，`ProxyRule{Protocol, Upstream, Ports, Nets}`。
- `sidecar` 契约：JSON over HTTP/stdin（ADR-039 §1）。

### 3.3 ui（前端型）

| 字段 | 语义 |
|---|---|
| `nav[]` | 侧边栏入口 `{path, label, icon}`；内核据此动态注册路由/菜单 |
| `routes[]` | 额外路由声明 |
| `slots[]` | 具名合并点 `{slot, from}`（L0 元数据驱动） |
| `page` | 页面类型（如 `settings`） |

详见 [插件 UI 指南](plugin-ui.md)。

### 3.4 data（数据订阅）

`subscribe: [domains, certs, ports, services]` — 声明消费哪些投影；核心经投影 API（只读、带 revision）下发。

## 4. 权限 permissions

| 键 | 语义 | 强制方 |
|---|---|---|
| `filesystem.write[]` | 路径白名单 | 内核实际强制 |
| `api[]` | scope 列表，如 `domain:read`、`gateway:read` | 内核按 plugin token 校验 |
| `network[]` | 网络需求声明 | 安装确认页展示 |

安装时生成 **plugin token**，绑定 `api` scope；插件不共享管理员 session（ADR-039 §2）。

## 5. 版本化政策

| 变更 | 版本影响 |
|---|---|
| 贡献点新增**可选**字段 | `extensionApi` minor |
| 删除字段 / 改变语义 | `extensionApi` **major**（须在主仓库写 ADR） |
| 新增 point | minor |

- 客户端按 `requires.extensionApi` 过滤；不兼容的 manifest 拒绝加载。
- 修改本仓库 `schema/` **必须**先在主仓库落地（见 `AGENTS.md` 致命顺序）。

## 6. 中性模型

内核只产出中性对象，不解析插件语法：

```go
type ProxyRule struct {
    Protocol string   // https / mqtt ...
    Upstream string   // 后端 IP:端口
    Ports    []int
    Nets     []string // tcp / udp
}
```

变体键算法见 [catalog.md §3](catalog.md#3-配方变体variants)。
