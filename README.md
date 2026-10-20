# WorkBuddy2API

**把 CodeBuddy 账号变成 OpenAI 兼容 API 的多账号网关**

> [!TIP]
> 仅限本人授权账号、本机、私有环境使用，勿暴露到公网，否则一切后果自负，本项目仅供学习用途，随时可能删库

项目源自已删除的 Sliverkiss/workbuddy2api，保留其账号池、调度与改写管线。本项目另加：

- 内置管理面板
- 多 key 存储
- 纯 Go `wbapi` CLI、登录与任务工具
- Windows 计划任务部署
- 无 Python、Shell、Batch 运行时依赖
- 更多...

配置键与端点都是增量的，可直接替换上游二进制

## 快速开始

- [Windows 原生部署（计划任务）](docs/deploy.md#windows-原生部署计划任务)
- [源码构建](docs/deploy.md#源码构建)
- [源码与运行目录分离](docs/deploy.md#源码与运行目录分离)

## 文档

- [部署](docs/deploy.md)：Windows 计划任务、源码构建、运行目录分离、验证
- [核心能力](docs/features.md)：账号池、流量治理、选号语义、请求链路、定时任务、双域、架构图
- [管理后台与 CLI](docs/admin.md)：面板页面、多 key 存储、`wbapi` 命令表
- [配置](docs/config.md)：配置段要点、环境变量、fail-fast 行为
- [开发](DEVELOPMENT.md)：目录结构、前端构建、管理 API 约定、测试
- [安全与许可](docs/compliance.md)：使用边界、免责声明、许可

## 许可

MIT 许可，使用前请阅读[免责声明](docs/compliance.md)。
