# GateBoxStore

[GateBox](https://github.com/JiangBeta/gatebox) 的**官方插件仓库与静态索引**。托管插件源码、扩展契约 schema、构建脚本与 `index.json`；制品发布到 GitHub Releases。**无常驻服务端**。

- 插件是**数据**：`manifest v2 + 通用引擎`。安装第三方插件制品 = 执行代码，签名与校验和是安全底线。
- 本仓库是插件**源码与发布**的唯一入口；扩展契约的**权威定义在 GateBox 主仓库**（`plugins/schema/`、`registry/schema/` 与 ADR-037~039），本仓库 vendor 一份并校验一致。

## 仓库结构

```
GateBoxStore/
├── cmd/gbx-store/            # 维护工具：index（生成索引）/ pack（打包制品）
├── plugins/<id>/             # 插件源码（一插件一目录）
│   ├── manifest.yaml         # 声明（kind / requires / artifacts / contributions / permissions）
│   ├── backend/              # sidecar 源码（kind:process）
│   ├── ui/                   # 前端页面（L1 iframe）
│   └── assets/               # 静态资源（如 acme dnsapi 脚本）
├── schema/                   # vendor 自主仓库的扩展契约 schema
│   ├── manifest.v2.schema.json
│   └── catalog.v1.schema.json
├── docs/                     # 插件作者文档
│   ├── plugin-authoring.md   # 如何写一个插件
│   ├── catalog.md            # 索引、变体矩阵、发布流程
│   ├── extension-api.md      # 贡献点 / 权限 / 版本化 参考
│   └── plugin-ui.md          # 插件 UI（L0/L1）与 postMessage 桥
├── variants.yaml             # 核心组件配方变体清单（ADR-038）
├── index.json                # 生成的静态索引（发布到 GitHub Pages）
└── .github/workflows/        # CI：构建插件 / 构建变体 / 生成索引
```

## 插件形态

| kind | 用途 | 制品 | 有独立进程 |
|---|---|---|---|
| `caddy-module` | 核心插件扩展：给核心组件加编译期模块（声明中性 `feature`，走配方变体） | 无整包二进制（变体由本仓库构建） | 否 |
| `process` | 独立进程：自带后端二进制与页面 | `sidecar` + `ui` | 是 |
| `config-only` | 纯声明式贡献（如 `dns-provider`） | 无 | 否 |

## 快速开始（插件作者）

1. 阅读 [docs/plugin-authoring.md](docs/plugin-authoring.md)；
2. 复制 `plugins/caddy-l4/` 作为参考，创建 `plugins/<id>/`；
3. 按 [docs/extension-api.md](docs/extension-api.md) 编写 `manifest.yaml`；
4. 本地生成索引预览：`go run ./cmd/gbx-store index`；
5. 提交 PR。合并后 CI 构建制品、发布 Release、更新 `index.json`。

## 构建与发布

```bash
scripts/build-plugin.sh <id>          # 构建 sidecar/ui/assets → dist/<id>/
go run ./cmd/gbx-store stamp \
  -plugin plugins/<id> -dist dist/<id> \
  -base-url https://github.com/JiangBeta/GateBoxStore/releases/download/<id>/v<ver>
go run ./cmd/gbx-store index          # 重新生成 index.json
```

- `build-plugin.sh`：Go sidecar（`backend/`）+ Vite UI（`ui/`）或静态 UI + `assets/`。
- `stamp`：按 role 约定名字回填 `artifacts[].url/sha256/size`（保留 manifest 注释）。
- 打 tag `<id>/v<semver>` 后，CI（`release-plugin.yml`）自动构建 → 回填 → 发布 Release → 提交索引。

## 文档

- [插件作者指南](docs/plugin-authoring.md)
- [索引与发布](docs/catalog.md)
- [扩展 API 参考](docs/extension-api.md)
- [插件 UI 指南](docs/plugin-ui.md)
- 宿主设计：[ADR-037 主程序与插件分离](https://github.com/JiangBeta/gatebox/blob/main/docs/adr/ADR-037.md) · [ADR-038 配方变体](https://github.com/JiangBeta/gatebox/blob/main/docs/adr/ADR-038.md) · [ADR-039 插件运行时契约](https://github.com/JiangBeta/gatebox/blob/main/docs/adr/ADR-039.md)

## 治理

- 信任分级：`official`（本仓库维护）/ `verified` / `community`；决定可用 UI 层（L0/L1/L2）与权限。
- 契约权威在主仓库：修改 `schema/` 前需先在 GateBox 主仓库更新并升级 `extensionApi`（新增可选字段 = minor，破坏性变更 = major，须写 ADR）。
