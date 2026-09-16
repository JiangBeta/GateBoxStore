# schema/（vendor）

从 GateBox 主仓库 vendor 的扩展契约 schema。**权威定义在主仓库**，本目录只是副本，供本仓库校验与插件作者查阅。

| 文件 | 主仓库源 | 契约版本 |
|---|---|---|
| `manifest.v2.schema.json` | `plugins/schema/manifest.v2.schema.json` | v2 |
| `catalog.v1.schema.json` | `registry/schema/catalog.v1.schema.json` | v1 / `extensionApi: 1` |

## 修改流程（强制）

1. 先在 `JiangBeta/gatebox` 主仓库修改 schema，并评估 `extensionApi` 版本（新增可选字段 = minor，破坏性 = major + ADR）；
2. 主仓库合并后，把文件同步到本目录；
3. 若涉及贡献点变更，更新 `docs/extension-api.md` 与受影响插件；
4. CI 校验两仓文件一致（`schema-check` job）。

**禁止只改本目录。** 详见 `AGENTS.md` 致命顺序。
