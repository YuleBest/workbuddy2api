# 开发说明

给仓库维护者与贡献者。部署与使用看 `README.md`。

## 目录结构

```
cmd/server       网关主程序（配置加载、pool/scheduler 组装、HTTP 服务）
cmd/login        设备授权登录 CLI
cmd/signin       单次签到 CLI
cmd/credit       积分查询 CLI
cmd/activity     活跃上报一次性触发器
internal/admin   管理后台：观测 API + 运维动作 + 内嵌 SPA（dist 由 web/ 构建产出）
internal/server  数据面 handler + 上游的运维端点（admin.go：/admin/accounts/{uid}/*）
internal/auth    凭证解析与原子写回
internal/config  跨命令共享的配置段（schedule）与默认值
internal/pool    账号池：选号 / 冷却 / 熔断 / 在途租约 / 状态持久化
internal/scheduler 定时任务：签到 / 活跃上报 / 猫猫旅行 / token 保活
internal/server  数据面：OpenAI 兼容 handler、鉴权、请求日志
internal/session 会话粘性路由
internal/upstream 上游客户端：SSE 流、请求体改写管线、错误分类
web              Vue 3 + Vite + TS 管理面板源码
```

## 环境要求

- Go ≥ 1.22（仓库当前用 1.24 开发）
- Node ≥ 20 + pnpm ≥ 9（只改后端时不需要）
- 一个已注册的 CodeBuddy 账号（跑真实请求时才需要）

## 常用命令

```bash
# 后端
go build ./...
go vet ./...
go test ./...            # 完整测试套件
go test -race ./internal/admin/ ./internal/pool/ ./internal/server/

# 前端（web/ 目录）
pnpm install
pnpm dev                 # Vite 开发服务器，/api/admin 代理到 127.0.0.1:7863
pnpm build               # 类型检查 + 构建，产物落 internal/admin/dist（被 go:embed 拾取）
pnpm build:only          # 跳过类型检查的快速构建
```

## 管理后台的构建链路

```
web/src/*.vue ──vite build──▶ internal/admin/dist/ ──go:embed──▶ 二进制
```

- `internal/admin/dist` 是构建产物，**提交进版本库**：这样只改后端的人（以及 Dockerfile 的
  `go build`）不需要 Node 就能编译。
- 改完前端必须重新 `pnpm build` 再 `go build`，否则二进制里还是旧页面。
- `internal/admin/webui.go` 负责静态资源与 SPA 回退：命中文件按扩展名给 Content-Type，
  `assets/` 下（内容哈希文件名）长缓存，其余 `no-cache`，未知路径回退 `index.html` 交给前端路由。
- 前端路由是 history 模式，base 固定 `/admin/`；Go 侧的路由前缀与 `vite.config.ts` 的 `base`
  必须一致，换前缀要同时改。

## 两套管理入口的关系

网关有两组管理端点，语义同源、客户端不同，**不要各写一套状态机**：

| 入口 | 路径 | 客户端 |
|---|---|---|
| 上游运维端点 | `POST /admin/accounts/{uid}/{disable,enable,revive}` | `acct.sh` / `cmd/acct` CLI |
| Web 后台 API | `GET/POST /api/admin/*` | 浏览器面板（`web/`） |

- 两边都走 `pool` 的同一批方法：`SetManualDisabled`（手动停用位）/ `ReviveDisabled`（系统自动禁用位）/
  `ManualDisabledState`（读双位）。**手动停用与系统自动禁用是两个独立状态位**：`enable` 只解手动位，
  仍被系统禁用的号需要 `revive` 才回选号池。
- 上游端点的模式表在 `internal/server/handler.go` 的 `adminRoutes`，经 `server.AdminRoutePatterns()`
  暴露给 `cmd/server`：后台前端挂在 `/admin/` 前缀下，必须先把这些模式转回数据面，
  否则会被 SPA 吃掉（`acct.sh` 会 404）。**上游新增管理端点时同步这张表即可，不用改 main.go。**
- 开关是同一个 `config.admin.enabled`（默认 false）。

## 管理 API 约定

- 全部挂在 `/api/admin/*`，Bearer token 鉴权（常量时间比较）。
- 响应一律 JSON；错误形如 `{"error":{"code":"...","message":"..."}}`。
- 只读接口：`session` / `overview` / `accounts` / `keys`(GET) / `logs` / `models` / `config`。
- 动作接口：账号 `disable|enable|revive|clear-cooldown`、密钥 `POST` / `DELETE`、
  `tasks/{checkin|activity|travel|keepalive}`。
- `tasks/*` 默认异步（202，结果写服务日志）；`tasks/checkin?wait=1` 同步等待并回传逐号回执
  （签到是秒级短任务，公网隧道等得起；旅行/活跃上报按账号限速，同步会撑到分钟级，故不支持 wait）。
- 动作接口只调用 `pool` / `scheduler` / `server.KeyStore` 的既有入口，不直接改内部状态；
  新增动作时优先复用这些入口，别在 admin 包里另写一套状态机。
- 脱敏：`config` 接口不回传任何密钥明文（只给 `*_set` 布尔）；`accounts` 里 uid 全量返回给管理端
  （运维需要区分账号），但**日志**里只打前 8 位。

## 测试

- 后端：`go test ./...`。新增管理接口时在 `internal/admin/admin_test.go` 里加一条 httptest 用例，
  覆盖"无 token 401 / 动作生效 / 未知目标 404"三类断言。
- 请求日志环形缓冲的测试在 `internal/server/reqlog_test.go`（顺序、容量回绕、脱敏）。
- 密钥读写测试在 `internal/server/keystore_test.go`（新增/掩码/前缀吊销/坏文件不覆盖）。
- 前端：`pnpm build` 内含 `vue-tsc --noEmit` 类型检查；改完 UI 用 `pnpm dev` 或构建后进
  `/admin/` 实际点一遍（尤其动作按钮与空/错状态）。

## 提交前自检

```bash
gofmt -l $(git ls-files '*.go')   # 应为空（仓库历史上有少量未格式化文件，别顺手全量重排）
go vet ./...
go test ./...
cd web && pnpm build              # 含类型检查
```

> 注意：仓库里有若干**历史遗留**的 gofmt 偏差文件。只格式化你改动的文件，避免整仓重排
> 制造无意义的 diff。
