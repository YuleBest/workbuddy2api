<p align="center"><img src="https://raw.githubusercontent.com/DGZSbot/ai-icon/refs/heads/main/WorkBuddy.png" alt="WorkBuddy2API" width="110"></p>

<h1 align="center">WorkBuddy2API</h1>

<p align="center"><b>把 CodeBuddy 账号变成 OpenAI 兼容 API 的多账号网关</b></p>

> [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api) 的 fork，由 [@YuleBest](https://github.com/YuleBest) 维护。
> 上游的账号池、调度与改写管线原样保留，另加内置管理面板、多 key 存储、`wbapi` CLI 与 Windows 计划任务部署。
> 配置键与端点都是增量的，可直接替换上游二进制。

自托管网关：OAuth 登录 CodeBuddy 账号 → 账号池轮转 / 冷却熔断 / 会话粘性 / 积分补充 → 只对客户端暴露
`/v1/chat/completions`，SDK 零改造接入。只做上游网关，不做下游协议转换与多租户；
**仅限本人授权账号、本机 / 私有环境使用**。

## 快速开始

```bash
git clone https://github.com/YuleBest/workbuddy2api.git && cd workbuddy2api
cp config.example.json config.json    # 至少设 api_key（留空 = 不鉴权）
./login.sh                            # 浏览器完成设备授权，可重复加号
docker compose up -d --build
curl -s localhost:7863/healthz        # 无可用账号时 503
```

Windows 用计划任务 + `server.exe`，见[部署](docs/deploy.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [核心能力](docs/features.md) | 账号池、流量治理、选号语义、请求链路、定时任务、双域、架构图 |
| [部署](docs/deploy.md) | Docker Compose、Windows 计划任务、源码构建、验证 |
| [管理后台与 CLI](docs/admin.md) | 面板七个页面、多 key 存储、`wbapi` 命令表 |
| [配置](docs/config.md) | 配置段要点、环境变量、fail-fast 行为 |
| [开发](DEVELOPMENT.md) | 目录结构、前端构建、管理 API 约定、测试 |
| [安全与许可](docs/compliance.md) | 使用边界、免责声明、MIT |

## 许可

MIT 许可，使用前请阅读[免责声明](docs/compliance.md)。
