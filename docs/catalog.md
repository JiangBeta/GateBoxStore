# 索引、变体矩阵与发布

> 本仓库产出 `index.json`（静态索引），GateBox 客户端据此在线安装/升级。无服务端。
> 设计：ADR-037 §6（GateBoxStore）、ADR-038（配方变体）、ADR-039 §4（分发与发现）。

## 1. 产物

| 文件 | 生成方式 | 用途 |
|---|---|---|
| `plugins/<id>/manifest.yaml` | 手写 | 插件声明（权威） |
| `variants.yaml` | 手写 + CI 回填 sha256 | 核心组件配方变体清单 |
| `index.json` | `go run ./cmd/gbx-store index` | 发布给客户端的静态索引 |
| GitHub Releases 制品 | CI 构建 | `role:binary/sidecar/ui/assets` 的 tar.gz |

## 2. catalog v1 形状

```json
{
  "schema": "gatebox.catalog/v1",
  "extensionApi": 1,
  "generatedAt": "2026-09-16T00:00:00Z",
  "publisher": { "id": "jiangbeta", "name": "GateBox", "keyID": "..." },
  "plugins": [
    {
      "id": "caddy-l4",
      "versions": [
        {
          "version": "0.1.0",
          "channel": "official",
          "publishedAt": "2026-09-16T00:00:00Z",
          "extensionApi": 1,
          "manifest": {},
          "artifacts": [],
          "signature": "..."
        }
      ]
    }
  ],
  "variants": [
    {
      "key": "sha256:1a2b3c4d5e6f7a8b",
      "component": "caddy",
      "version": "2.10.0",
      "features": ["github.com/mholt/caddy-l4"],
      "os": "linux",
      "arch": "amd64",
      "url": "https://github.com/JiangBeta/GateBoxStore/releases/download/variants/caddy-2.10.0-l4-linux-amd64.tar.gz",
      "sha256": "...",
      "size": 12345678
    }
  ]
}
```

- Schema 权威：`schema/catalog.v1.schema.json`（vendor 自主仓库）。
- 每个插件版本内嵌完整 **manifest v2**。
- 制品可打包为单一 tar.gz，或独立 URL（GitHub Releases）。

## 3. 配方变体（variants）

核心插件扩展（`caddy-module`）**不发布整包二进制**，只声明中性特征。CI 按特征并集构建变体：

**变体键算法（两侧必须一致）**：

```
canonical = "component=" + component + "\n"
          + "version="   + version   + "\n"
          + "os="        + os        + "\n"
          + "arch="      + arch      + "\n"
          + 每个特征一行 "feature=" + f   （特征去重后按字节序升序）
key = "sha256:" + hex(sha256(canonical))[:16]
```

- 特征、组件名、版本、os、arch 先 `trim`；特征去重后排序。
- GateBox 内核用同一算法计算期望键，再在 `variants[]` 中查找；命中即下载替换，未命中则按需构建或导入 `custom` 制品（ADR-038 §4）。

`variants.yaml`：

```yaml
variants:
  - component: caddy
    version: "2.10.0"
    features: ["github.com/mholt/caddy-l4"]
    os: linux
    arch: amd64
    url: https://github.com/JiangBeta/GateBoxStore/releases/download/variants/caddy-2.10.0-l4-linux-amd64.tar.gz
    sha256: "..."
```

`gbx-store index` 会在 `key` 缺省时按上述算法补全。

## 4. 发布流程

```
PR（plugins/<id>/ 或 variants.yaml）
        │
        ▼  merge
CI: build-plugins.yml
   ├─ 构建 sidecar / ui（pnpm build）/ assets
   ├─ 打包 tar.gz（gbx-store pack）→ 计算 sha256
   ├─ 生成 manifest 的 artifacts[].url/sha256 回填
   └─ 发布 GitHub Release（tag: <id>/v<version>）
        │
        ▼
CI: gbx-store index → 提交 index.json（客户端经 raw.githubusercontent 直链拉取）
```

- **tag 约定**：插件 `<id>/v<semver>`；变体 `variants/<component>-<version>-<features-hash>`。
- **同版本不可覆盖**：已发布的 `<id>/v<version>` tag 与其制品不可变。

## 5. 客户端流程

```
启动/手动检查
  └─ 磁盘 manifest 加载（内置）
  └─ 拉 index.json（默认指向本仓库，可覆盖）
      └─ 过滤 extensionApi / requires.gatebox 不兼容项
      └─ 按 channel 比较版本
      └─ 下载制品 → 校验 sha256 → 原子安装 → 注册贡献
```

## 6. 通道与信任分级

- `channel`：`official` / `stable` / `beta`；客户端默认取 `official`，可切 `beta`。
- 信任分级（`publisher` + 签名，后者本阶段预留）：`official`（本仓库）/ `verified` / `community`。
  - 决定可用 UI 层：`community` 仅 L0/L1；`official`/`verified` 可 L2 远程 ESM。
  - 决定权限默认值：高权限需用户显式确认。

## 7. 签名（Ed25519）

索引 `index.json` 使用 **Ed25519 detached 签名**（`index.json.sig`，base64）：

```bash
go run ./cmd/gbx-store keygen -out keys      # 生成 keys/catalog.key(私钥) 与 keys/catalog.pub(公钥)
go run ./cmd/gbx-store sign -in index.json    # 生成 index.json.sig
```

- 私钥配到 CI Secret `GBX_STORE_KEY`（`sign-index.yml` 自动签名并提交 `index.json.sig`）；**私钥绝不入库**（`.gitignore` 已忽略 `keys/`、`*.key`）。
- 索引默认地址：`https://raw.githubusercontent.com/JiangBeta/GateBoxStore/main/index.json`（签名在 `<地址>.sig`）。
- 公钥配到 GateBox 的 `catalog_pubkey`（base64）；配置后客户端**强制验签**，签名缺失或错误即拒绝加载索引。
- 未配置公钥时客户端不验签（开发/内网场景），但仍校验每个制品的 `sha256`。
- 制品级签名仍预留（`signature` 字段）；当前以索引签名 + `sha256` 为安全边界。
