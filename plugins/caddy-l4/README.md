# Caddy L4

为 Caddy 增加 TCP/UDP（L4）代理能力。

- **kind**：`caddy-module`（核心插件扩展）
- **依赖**：`caddy` 组件；`gatebox >= 0.4.0`；`extensionApi >= 1`

## 它做什么

- 声明中性特征 `github.com/mholt/caddy-l4`；GateBox 把它加入 Caddy 的**特征并集**，由 GateBoxStore CI 构建含该特征的 Caddy 变体二进制（ADR-038）。**本插件不提供整包二进制**，因此可与 `coraza` 等同类插件共存——二者会合并为一个变体。
- 注册 `proxy-protocols` 能力（`class: non-http`，TCP/UDP），前端「端口」表单据此出现非 HTTP 协议选项。
- 提供 `renderer`（声明式模板）：把中性 `ProxyRule` 渲染为 Caddyfile 的 `layer4 { ... }` 全局块。

## 使用

1. 在 GateBox「扩展 → 插件」安装并启用本插件；
2. 系统会切换 Caddy 为含 L4 的变体并重启 Caddy（首次可能需等待变体构建）；
3. 在网关「端口」页创建 TCP/UDP 代理即可。

## 卸载

停用/卸载后，若不再有其他 Caddy 扩展插件，Caddy 会重算回纯核心变体（ADR-037 §4）。

## 权限

- `api: [gateway:read]`

## 维护

- 目录：[`plugins/caddy-l4/`](./)
- 参考：[插件作者指南](../../docs/plugin-authoring.md) · [扩展 API 参考](../../docs/extension-api.md)
