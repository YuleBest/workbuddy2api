# 部署

[← 返回 README](../README.md)

## Docker Compose 一键部署

- Docker + Docker Compose

```bash
git clone https://github.com/YuleBest/workbuddy2api.git
cd workbuddy2api
cp config.example.json config.json
```

然后编辑 `config.json`，至少设置 `api_key`（留空不鉴权）

```bash
# 登录添加账号（重复执行可加多号）
# 注意：执行过下方说明中的 chown 后，host 侧 login.sh 会被可写性预检拦截，此时请在容器内登录，见下方说明）
./login.sh 

# 启动服务
docker compose up -d --build

# 健康检查
# 无可用账号时 503，service 字段用于确认打到的是本网关
curl -s http://localhost:7863/healthz
# {"healthy":2,"total":3,"service":"workbuddy2api"}
```

`login.sh` 内置授权 URL 获取 + 浏览器登录 + token 轮询 + 首次签到 + 凭据落盘 + 容器重启，全程无 PKCE

<details>
<summary>非 root 宿主用户注意</summary>

`./login.sh` 以**当前宿主用户**落盘凭证，容器内网关却以 `app`（uid 10001）读 + 回写。两者 uid 不同时容器读不到凭证文件，`/status` 账号数为 0

登录后、启动前，由 root 或部署用户把目录属主交给 10001：

```bash
chown -R 10001:10001 ./auths
```

之后新增账号**必须**进**容器内**登录（`app` 自己落盘，属主即 10001，不用反复 chown）：

```bash
docker compose exec -it wb2api bash -c './login.sh' && docker compose restart wb2api
```

chown 之后 host 侧 `./login.sh` 已无写权限，脚本会在拉起浏览器授权前退出并提示，不会白走一遍 OAuth；容器内没有 docker CLI，登录完回宿主机重启。

</details>

## Windows 原生部署（计划任务）

- Windows 10/11
- Python 3（仅 `wbapi` CLI 需要）。

```powershell
Copy-Item config.example.json config.json
# 编辑 config.json；建议把 listen 设为 127.0.0.1:7863，且务必设置 api_key

go build -trimpath -ldflags="-s -w" -o server.exe ./cmd/server
go build -trimpath -ldflags="-s -w" -o login.exe ./cmd/login
go build -trimpath -ldflags="-s -w" -o signin_bin.exe ./cmd/signin
go build -trimpath -ldflags="-s -w" -o credit.exe ./cmd/credit
```

把两个启动入口注册成计划任务（本仓库的约定名字是 `wbapi-gateway` / `wbapi-tunnel`，`wbapi` 的服务命令按这两个名字操作）：

- `gateway-task.cmd` — 启动 `server.exe -config config.json`，PID 写 `gateway.pid`，日志写 `gateway.log` / `gateway.err.log`
- `tunnel-task.cmd` — 启动 `cloudflared.exe` 隧道，PID 写 `tunnel.pid`

```powershell
# 以「登录时触发」注册（示例；按需改成开机触发或加延迟）
schtasks /Create /TN wbapi-gateway /SC ONLOGON /TR "C:\path\to\wb2api-fork\gateway-task.cmd" /F
schtasks /Create /TN wbapi-tunnel  /SC ONLOGON /TR "C:\path\to\wb2api-fork\tunnel-task.cmd"  /F
```

这两个 `.cmd` 的内容是本机绝对路径，已被 `.gitignore` 排除，请按自己的目录改写。之后统一用 `wbapi` 操作：

```bash
wbapi status      # 网关 / 隧道 / 池子状态
wbapi start       # = schtasks /Run /TN wbapi-gateway + tunnel
wbapi restart     # 重启（隧道会闪断几秒）
wbapi log 50      # 网关日志尾部
```

<details>
<summary>补充说明</summary>

`wbapi` 是 Python 3 脚本，shebang 依赖 `python3`。Windows 上 `python3` 常被 Microsoft Store
的应用执行别名占用（**执行没有任何输出也不报错**），脚本就没法靠 shebang 直接跑。
放一个薄包装到 PATH 上，用真实解释器调它即可：

```sh
#!/bin/sh
# ~/bin/wbapi —— 用真实解释器调用仓库里的 wbapi
exec uv run --no-project --python 3.12 python "C:/path/to/wb2api-fork/wbapi" "$@"
```

没有 uv 的话，把 `uv run --no-project --python 3.12 python` 换成任何真实 Python 3 的绝对路径。
Linux / macOS 上 `python3` 通常没被劫持，直接 `./wbapi <命令>` 即可。

添加账号在 Git Bash 中运行 `login.sh`（它还负责 CN 首次签到以及 Global 注册地区 / trial 流程），或在浏览器里用面板的「账号池」页观察结果。

</details>

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
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o server ./cmd/server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o signin_bin ./cmd/signin
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o login ./cmd/login
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o credit ./cmd/credit
```

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
./acct.sh list && ./acct.sh disable <uid> 观察几天 && ./acct.sh enable <uid>

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
