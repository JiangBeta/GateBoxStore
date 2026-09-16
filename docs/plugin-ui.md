# 插件 UI 指南

> 插件前端如何被宿主加载、如何与宿主通信。设计：ADR-039 §3（UI 宿主与 postMessage 桥）、ADR-036 §7（三档渲染）。

## 1. 三档渲染

| 层 | 形态 | 隔离 | 适用 |
|---|---|---|---|
| **L0 元数据驱动** | 内核按 `slots` / schema 渲染 | — | 表单型扩展（协议选项、凭证字段、设置项） |
| **L1 iframe + postMessage** | 插件自带 UI 资源，内核内嵌 | 强隔离（独立源，仅经桥通信） | 复杂页面（如 mosdns 管理页） |
| **L2 远程 ESM** | 内核动态 import 插件 ESM | 无（同源 = 等价 XSS） | 仅 `official`/`verified`（**后置**） |

本阶段实现 L0 与 L1。优先选 L0；只有 L0 表达不了的复杂交互才用 L1。

## 2. L0：元数据驱动

在 manifest 声明 `ui.slots` 与 `page`，内核按内置 schema 渲染：

```yaml
contributions:
  ui:
    slots:
      - { slot: port-form.protocol-options, from: proxy-protocols }
    page: { type: settings }
```

配合 `capabilities` 的 `data`（如 `proxy-protocols`），前端自动出现对应控件。**无需写任何前端代码**。

## 3. L1：iframe + postMessage

### 3.1 制品与托管

```yaml
artifacts:
  - role: ui
    format: iframe
    url: https://github.com/JiangBeta/GateBoxStore/releases/download/mosdns/v0.1.0/mosdns-ui.tar.gz
    sha256: "..."
```

- 安装到 `$DATA_DIR/tools/<id>/ui/`；
- 内核静态托管于 `/plugins/<id>/`（与 API 同源）；
- 入口默认 `index.html`（可用 `entry` 指定）。

### 3.2 业务调用

插件 UI **只调用同源 API**，不直连宿主内部接口：

```
/api/v1/plugins/<id>/*   → 内核反代到你的 sidecar
```

宿主在 `init` 消息中下发 **plugin token**；所有请求带该 token。**不要依赖管理员 session**。

### 3.3 postMessage 桥（唯一宿主通道）

桥**只承载宿主能力，不传业务数据**。消息格式统一为 `{ type, ...payload }`。

| 方向 | `type` | 载荷 | 语义 |
|---|---|---|---|
| 宿主 → 插件 | `init` | `{ pluginId, token, theme, locale, apiBase }` | iframe 加载后宿主下发 |
| 插件 → 宿主 | `ready` | `{}` | UI 就绪 |
| 插件 → 宿主 | `resize` | `{ height }` | 内容高度变化，宿主调整 iframe |
| 插件 → 宿主 | `navigate` | `{ path }` | 请求宿主路由跳转 |
| 插件 → 宿主 | `toast` | `{ kind, message }` | 请求宿主提示 |
| 插件 → 宿主 | `setTitle` | `{ title }` | 设置当前页标题 |

最小实现示例（插件侧）：

```ts
window.addEventListener('message', (e) => {
  if (e.data?.type === 'init') {
    const { token, apiBase, theme } = e.data
    // 用 token 调 apiBase；按 theme 切换主题
  }
})
window.parent.postMessage({ type: 'ready' }, '*')
new ResizeObserver(() => {
  window.parent.postMessage({ type: 'resize', height: document.body.scrollHeight }, '*')
}).observe(document.body)
```

### 3.4 约束（必须遵守）

- **不读宿主 store、不触碰宿主 DOM、不 import 宿主模块**；与宿主只经 API/桥。
- 不假设 iframe 与宿主同 `window`；不访问 `window.parent` 的内部变量。
- 主题：按 `init.theme` 适配明暗；颜色/字号用自有 token，避免硬编码成宿主品牌色。
- 尺寸：内容高度自适应并上报 `resize`；不要自行滚动整页。
- 无网络直连宿主未声明的主机；外部请求需在 `permissions.network` 声明。

## 4. L2：远程 ESM（后置）

仅 `official`/`verified` 可用。内核动态 `import()` 插件入口 ESM，插件组件挂载到内核路由。**因同源故无隔离，等价于让插件执行任意前端代码**，需信任分级门禁与用户确认。本阶段不开放。

## 5. 构建 UI 制品

推荐 Vite（与本仓库无关的独立前端工程，产物为静态文件）：

```bash
# plugins/<id>/ui/
pnpm install && pnpm build          # 输出 dist/
tar -czf mosdns-ui.tar.gz -C dist .
sha256sum mosdns-ui.tar.gz          # 回填 manifest artifacts[].sha256
```

- 尽量**纯静态**（无 SSR、无 Node 运行时依赖）；
- 产物大小影响安装与加载，OpenWrt 等低资源设备尤需精简；
- 同一 UI 制品可按 `format` 区分：`iframe`（本期）或 `esm`（L2，后置）。
