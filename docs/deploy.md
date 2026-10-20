# 部署

[← 返回 README](../README.md)

## 预构建发布包

GitHub Release 会提供以下 6 个运行包，无需在本机安装 Go：

- `workbuddy2api-<tag>-linux-amd64.tar.gz`
- `workbuddy2api-<tag>-linux-arm64.tar.gz`
- `workbuddy2api-<tag>-darwin-amd64.tar.gz`
- `workbuddy2api-<tag>-darwin-arm64.tar.gz`
- `workbuddy2api-<tag>-windows-amd64.zip`
- `workbuddy2api-<tag>-windows-arm64.zip`

每个包包含完整运行命令、`config.example.json`、`README.md` 和 `LICENSE`。
Release 同时提供 `SHA256SUMS`，下载后可先校验再解压。

## Windows 原生部署（计划任务）

- Windows 10/11
- Go 1.22+（只用源码构建）

```powershell
Copy-Item config.example.json config.json
# 编辑 config.json；建议把 listen 设为 127.0.0.1:7863，且务必设置 api_key

go run ./cmd/build
```

把两个启动入口注册成计划任务（本仓库的约定名字是 `wbapi-gateway` / `wbapi-tunnel`，`wbapi` 的服务命令按这两个名字操作）：

- `wbapi-gateway` — 直接执行 `server.exe -config config.json`
- `wbapi-tunnel` — 直接执行 `cloudflared.exe` 与本机隧道参数

```powershell
# 以「登录时触发」注册（示例；按需改成开机触发或加延迟）
schtasks /Create /TN wbapi-gateway /SC ONLOGON /TR "\"C:\path\to\wb2api-fork\server.exe\" -config config.json" /F
schtasks /Create /TN wbapi-tunnel  /SC ONLOGON /TR "\"C:\path\to\cloudflared.exe\" tunnel run" /F
```

计划任务按本机绝对路径和真实隧道参数创建。之后统一用 `wbapi.exe` 操作：

```bash
wbapi status      # 网关 / 隧道 / 池子状态
wbapi start       # = schtasks /Run /TN wbapi-gateway + tunnel
wbapi restart     # 重启（隧道会闪断几秒）
wbapi log 50      # 网关日志尾部
```

`wbapi.exe`、`login.exe` 等全部由 Go 构建器生成，不依赖 Python。添加账号运行
`.\login.exe --realm=cn` 或 `.\login.exe --realm=global`；它同时负责 CN 首次签到和
Global 注册地区 / trial 流程。凭证写入 `auths/` 后网关会自动热加载，不需要重启。

## 源码构建

- Go ≥ 1.22
- 改前端还需 Node ≥ 20 + pnpm ≥ 9

管理面板是 Vue 3 单页应用，构建产物经 `go:embed` 打进二进制（仓库内已含一份构建产物，只改后端时可直接编译）。**改动 `web/` 下的前端代码后**，要先重新构建前端：

```bash
cd web && pnpm install && pnpm build
cd .. && go build ./cmd/server
```

开发前端时用 `pnpm dev` 起 Vite 开发服务器（已配置把 `/api/admin` 代理到 `127.0.0.1:7863`），改完再 `pnpm build` 落到二进制里。

```bash
go build ./...
go vet ./...
go test ./...      # 完整测试套件
go run ./cmd/server -config config.json
```

构建二进制：

```bash
go run ./cmd/build
```

构建产物默认写入 `dist/`。该命令一次构建服务、运维 CLI、任务命令以及 `login.sh` / `signin.sh` / `credit.sh` /
`trial.sh` / `acct.sh` / `checkin.sh` / `dev.sh` 等旧 Unix 命令名。它们都是 Go ELF，
不是 Shell 脚本。

## 源码与运行目录分离

生产目录和开发目录应分开，避免配置、凭证、日志和二进制污染 Git 工作树：

```text
源码目录：  ~/projects/wb2api
运行目录：  ~/.local/share/wb2api
```

运行目录保存 `wb2api`、`wbapi`、兼容命令、`config.json`、`keys.json`、`auths/`、
`data/` 和 `cloudflared/`。systemd 单元示例：

```ini
[Service]
User=yule
WorkingDirectory=/home/yule/.local/share/wb2api
ExecStart=/home/yule/.local/share/wb2api/wb2api -config /home/yule/.local/share/wb2api/config.json
Restart=on-failure
RestartSec=5
```

升级时从源码目录构建到临时 staging，再由 detached systemd 任务原子安装到运行目录并重启。
不要在源码目录直接运行生产网关。

## 验证

```bash
# 模型列表
curl -s http://localhost:7863/v1/models -H "Authorization: Bearer your-api-key"

# 账号状态（汇总 + 每账号详情，含 disabled / manual_disabled 双位）
curl -s http://localhost:7863/status -H "Authorization: Bearer your-api-key"

# 临时停用一个账号（需 config 里 admin.enabled = true）
curl -s -X POST http://localhost:7863/admin/accounts/<uid>/disable \
  -H "Authorization: Bearer your-api-key" -H "Content-Type: application/json" \
  -d '{"reason":"观察几天"}'
# 或用 CLI（自动从 config.json 读网关地址与 key）
./acct list && ./acct disable <uid> 观察几天 && ./acct enable <uid>

# 流式聊天
curl -sN http://localhost:7863/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"stream":true}'

# 非流式聊天（本地聚合）
curl -s http://localhost:7863/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"stream":false}'
```
