# WorkBuddy2API

把 CodeBuddy 与 WorkBuddy 账号接入 OpenAI 兼容接口的多账号网关。

> [!WARNING]
> 仅限本人已授权账号、本机或私有网络使用。不要把凭据、管理面板或网关端口暴露给未授权用户。

项目源自已删除的 Sliverkiss/workbuddy2api，目前独立维护。核心服务、CLI、登录和任务工具均使用 Go；Vue 管理面板只在构建前端时需要 Node。

## 核心特性

- 多账号池：按积分、过期时间和闲置状态分配请求，自动处理冷却、熔断和失败切换。
- OpenAI 兼容：支持流式与非流式对话、工具调用、会话粘性、提示词模式和双域模型。
- 内置管理面板：查看账号、密钥、日志、模型和运行状态，并执行常用运维动作。
- 纯 Go 运行：不需要 Python、Shell 或 Batch。`.sh` 兼容命令也是 Go 可执行文件。
- 原生发布包：GitHub Release 提供 Linux、macOS、Windows 的 `amd64` 与 `arm64` 版本。

本项目不提供 Docker 镜像。

## 快速开始

Linux 一键安装：

```bash
curl -fsSL https://raw.githubusercontent.com/YuleBest/workbuddy2api/master/install.sh | bash
```

安装器会下载当前版本的 Linux Release 包，校验 SHA-256，安装到用户目录，并打印管理面板地址。
它不会创建系统服务、启动计划或 cloudflared 配置。

其他平台可以从 [GitHub Releases](https://github.com/YuleBest/workbuddy2api/releases/latest) 下载对应压缩包。

完整步骤见[部署文档](docs/deploy.md)。

## 文档

- [部署](docs/deploy.md)：Release 安装、Linux/macOS/Windows 运行、源码构建和运行目录分离。
- [配置](docs/config.md)：常用配置段、环境变量和生效规则。
- [管理后台与 CLI](docs/admin.md)：面板功能、调用密钥和 `wbapi` 命令。
- [核心能力](docs/features.md)：账号池、请求链路、定时任务、双域和架构。
- [开发说明](DEVELOPMENT.md)：目录、开发流程、内部约定、测试和发布。
- [安全与许可](docs/compliance.md)：授权范围、免责声明和 MIT 许可。

## 许可

本项目使用 MIT License。使用前请阅读[安全与许可](docs/compliance.md)。
