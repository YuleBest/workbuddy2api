package wbapicli

const helpText = `wbapi — WorkBuddy2API 网关管理（新版：双域 / 双位停用 / 内置面板）

用法:
  wbapi status              网关/隧道状态 + 探活（别名 check，默认动作）
  wbapi doctor              自检：unit/二进制、auths 属主与可读性、keys 权限、管理面前置、运行态
  wbapi accounts            账号池：域 / 积分 / 冷却 / 双位停用 / 限额台账
  wbapi quota               冷却与限额详情；quota test [模型] 实测模型可用性
  wbapi acct list           账号运维：list | disable <uid> [原因] | enable <uid> | revive <uid>
  wbapi task <任务> [--wait]  手动触发定时任务：checkin | activity | travel | keepalive | school | cat
  wbapi key list [--show]   列出调用 key（--show 显示完整值）
  wbapi key new [备注]      生成新 key（立即生效，无需重启）
  wbapi key revoke <前缀>   吊销 key（立即生效）
  wbapi models [--realm cn|global] [--more]
                            模型清单表格（名/ID/域/倍率/能力/上下文/输出）；--more 打完整字段
  wbapi stats [--reset]     请求统计（上游 /v1/stats；--reset 清零累计）
  wbapi panel               管理后台地址与登录提示
  wbapi config              生效配置（脱敏）
  wbapi log [N]             网关最近 N 行日志（默认 30，journalctl）
  wbapi start|stop|restart  网关 + 隧道
  wbapi tunnel [start|stop|restart|status]  单独管理 Cloudflare Tunnel
  wbapi help                显示本帮助

环境:
  WB2A_HOME          项目根目录（默认取本程序所在目录）
  WB2A_CONFIG        配置文件路径（默认 $WB2A_HOME/config.json）
  WB2A_PUBLIC_HOST   公网域名（仅用于状态显示，默认 aiapi.yule.best）
  WB2A_GATEWAY       直接指定网关地址，如 http://127.0.0.1:7863（覆盖配置里的 listen）

鉴权（与网关自身同口径）:
  /v1/* /status /admin/*  → 调用 key（keys.json 首个 key，或 config 的 api_key）
  /api/admin/*            → 管理 token（config.admin.token，缺省回落上面那把调用 key）

多账号流程:
  1. ./login.sh            浏览器完成设备授权（--realm=global 加国际版号）
  2. wbapi accounts        确认池里账号的域与状态
  之后网关自动按积分/成本/闲置加权轮转，撞配额自动换号。
`
