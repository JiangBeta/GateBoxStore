# Tailscale

跨网组网（独立进程插件，**attached**）。

- **kind**：`process`
- **构成**：仅 `sidecar`（观测/控制管理）+ `ui`（状态页）
- **attached 语义**：`tailscale` 本体由**系统包管理器**提供（`/usr/bin/tailscale`），GateBox **不安装、不更新其配方**；插件只调用其 CLI（对应 ADR-001/ADR-030 的 attached/system）。

## 工作方式

- `GET /status`：执行 `tailscale status --json`，映射为 `running / stopped / needs-login`，并取版本与本机地址。
- `POST /up` / `POST /down`：执行 `tailscale up` / `tailscale down`（`up` 可能需在浏览器完成授权，耗时较长）。
- 执行身份为 GateBox 服务用户；若权限不足，请在系统侧为 `tailscale` 配置 operator（`tailscale set --operator=<用户>`）。

## 构建

```bash
cd backend
go build -o ../../../dist/tailscale-sidecar ./
tar -czf ../../../dist/tailscale-sidecar-linux-amd64.tar.gz -C ../../../dist tailscale-sidecar
sha256sum ../../../dist/tailscale-sidecar-linux-amd64.tar.gz   # 回填 manifest sidecar.sha256
```

## 端点

`GET /status`、`POST /up`、`POST /down`。

## 权限

- `filesystem.write: [tools/tailscale]`
