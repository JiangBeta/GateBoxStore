# plugins/

一插件一目录。目录名 = `manifest.yaml` 的 `id`。

```
plugins/<id>/
├── manifest.yaml     # 必需（schema/manifest.v2.schema.json）
├── backend/          # kind:process 的 sidecar 源码
├── ui/               # L1 iframe 前端源码
├── assets/           # 静态资源（如 acme dnsapi 脚本）
└── README.md         # 面向用户的说明
```

## 迁移计划

从 GateBox 主仓库迁入的插件（见 ADR-037 §迁移试点，按三条路径验证）：

| 插件 | kind | 路径 | 状态 |
|---|---|---|---|
| `mosdns` | process | 独立进程（sidecar + L1 UI） | **已迁移**（后端/管理页/本体均在此） |
| `ddns-go` | process | 消费型（投影 + reconcile） | **已迁移**（sidecar 在此；无页面） |
| `caddy-l4` | caddy-module | 核心插件扩展（配方变体） | 清单已建 |
| `coraza` / `geoip` / `realip` | caddy-module / config-only | 声明式贡献 | 清单已建 |
| `flame` | process | 导航（iframe 页） | 待迁 |
| `tailscale` | — | 组网（attached/system） | 保留核心（不迁） |

## 约定

- `id` 发布后不可改（客户端按 id 识别）。
- 见 [插件作者指南](../docs/plugin-authoring.md) 与 [扩展 API 参考](../docs/extension-api.md)。
