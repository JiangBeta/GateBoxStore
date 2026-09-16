# Flame

应用导航面板（独立进程插件）。

- **kind**：`process`
- **构成**：`sidecar`（进程代管 + 状态）+ `binary`（本体）+ `ui`（内嵌页）
- **进程模型**：GateBox 只托管 sidecar；本体由 sidecar 代管（ADR-037 决策 (a)）
- **上游说明**：本体来自 **`soulteary/flare`**（发布资产名 `flare_*`），安装后统一落为 `tools/flame/flame`；插件 id 沿用项目既有命名 `flame`。

## 页面

内嵌页 `ui/index.html` 提供 flame 自身的导航面板（iframe 指向 flame Web 端口）与启停控制：

- 端口来自 `GATEBOX_FLAME_PORT`（默认 `5005`），sidecar 启动 flame 时以 `PORT` 环境变量注入；
- iframe 地址为 `http://<当前访问主机>:<port>/`，需该端口对用户浏览器可达（或经网关反代）。

## 构建

```bash
# 侧车
cd backend
go build -o ../../../dist/flame-sidecar ./
tar -czf ../../../dist/flame-sidecar-linux-amd64.tar.gz -C ../../../dist flame-sidecar
sha256sum ../../../dist/flame-sidecar-linux-amd64.tar.gz   # 回填 manifest sidecar.sha256

# 内嵌页（纯静态，无需构建）
tar -czf ../../../dist/flame-ui.tar.gz -C ui .
sha256sum ../../../dist/flame-ui.tar.gz                    # 回填 manifest ui.sha256
```

## 端点

`GET /status`、`POST /start|/stop|/restart`。

## 权限

- `filesystem.write: [tools/flame]`
