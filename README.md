# QQ 状态图机器人

一个专注于官方群聊状态查询和 GGAPI 账号余额查询的 QQ 开放平台机器人。群成员发送 `@机器人 状态`（或 `status`）后，服务读取 [GGAPI 状态页](https://status.ggapi.cc) 的公开 JSON API，用 Go 原生生成中文 PNG，并通过 QQ 官方消息接口回复；图片后会附带 QQ 原生按钮菜单，可继续查看状态、打开帮助或进入账号操作。文字命令仍保持兼容。启用账号功能后，还支持 `@机器人 绑定`、`余额`、`解绑`、`取消` 和 `重发`。

项目包含一个本地管理端，用于配置机器人、查看收发摘要日志和预览状态图；不依赖 Chromium、CDP、OneBot、LLBot 或数据库。

## 工作流程

```text
QQ 官方 Webhook
  -> Ed25519 验签；普通消息入队并 ACK
  -> 消息按钮在 3 秒内调用互动响应接口
  -> 单工作线程读取状态页 config 与 monitor API
  -> Go 原生生成 1280px 中文 PNG
  -> 官方分片上传图片，再发送带内嵌键盘的文本菜单
  -> 使用原消息 msg_id 或互动 event_id 被动回复群图片
```

状态 API 的两次请求并发执行，单次生成最多等待 15 秒、每个响应最多读取 2 MiB。PNG 字节直接上传到 QQ 官方预签名存储地址，不需要额外提供公网图片 URL。

## QQ 开放平台配置

1. 在 [QQ 开放平台](https://q.qq.com/) 创建机器人，取得 `AppID` 与 `AppSecret`。
2. 在事件订阅中启用 `GROUP_AT_MESSAGE_CREATE` 和 `INTERACTION_CREATE`（Intent 为 `INTERACTION (1<<26)`）；如果 QQ 群开启了“接收所有消息”全量模式，也要允许 `GROUP_MESSAGE_CREATE`，程序会通过 `mentions` 确认消息确实 @ 了机器人。
3. 将 HTTPS 回调地址设为 `https://你的域名/qqbot/events`。
4. 按开放平台要求配置服务器出口 IP 白名单。
5. 将机器人添加到测试群；群里发送 `@机器人 状态`，收到状态图后点击“查看状态”验证互动回调。

按钮使用消息内直接定义的 `keyboard.content`，无需在管理端维护键盘模板。固定动作采用回调按钮：QQ 推送 `INTERACTION_CREATE` 后，服务先调用 `PUT /interactions/{interaction_id}` 结束客户端 loading，再通过事件 ID 返回业务结果。邮箱和验证码属于用户输入，仍需按回复提示通过 `@机器人 + 内容` 发送。

QQ 官方只允许 Webhook 使用 HTTPS，且回调端口必须是 `80`、`443`、`8080` 或 `8443`。Compose 默认仅在宿主机 `127.0.0.1:8090` 监听，请使用现有反向代理提供 HTTPS。

## 启动

```bash
cp .env.example .env
docker compose up -d --build
```

首次打开 `/admin/` 设置管理密码，无需用户名；随后在「机器人配置」页填写 AppID、AppSecret、群白名单与状态图数据源。配置保存到 `./data/qq-status.json`，不会写入前端或日志。GGAPI 余额区域使用只读管理令牌和 SMTP 发送一次性验证码；管理端只展示脱敏绑定邮箱，可在「账号绑定」标签页撤销绑定。管理端的「生成预览」会先保存当前配置，再调用已鉴权的 `GET /api/status-preview` 返回实际 PNG。

「故障通知」区域可配置独立告警群、连续故障/恢复样本阈值，并在每个已保存的告警群旁直接测试发送。目标群向机器人发送一次消息后会出现在“已发现群”下拉框中，无需手动复制 `group_openid`。正式告警默认关闭，首次启用或更换数据源时只建立心跳基线。

「主动测试」区域可向已发现或已配置的群主动发送真实状态图，也可发送带“模拟测试”标识的故障与恢复通知。模拟操作只验证主动消息权限、通知内容和目标群可达性，不修改告警基线或节点故障状态。对应的已鉴权接口为 `POST /api/status/send` 和 `POST /api/alerts/simulate`。

告警消息中的时间统一按 `Asia/Shanghai`（UTC+08:00）显示，状态 API 返回的 UTC 时间也会转换后再发送。

健康检查：`GET /api/health`

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `QQBOT_APP_ID` | 空 | QQ 开放平台机器人 AppID，也可在管理端填写 |
| `QQBOT_APP_SECRET` | 空 | QQ 开放平台机器人 AppSecret，禁止提交到仓库 |
| `QQBOT_ALLOWED_GROUPS` | 空 | 允许查询的群 OpenID，逗号分隔；空表示全部已授权群 |
| `STATUS_COMMANDS` | `状态,status` | 命令列表，逗号分隔，完整匹配且英文不区分大小写 |
| `STATUS_URL` | `https://status.ggapi.cc` | 状态图公开 API 的基础 URL，仅支持 HTTP/HTTPS |
| `STATUS_PAGE_ID` | `default` | 状态页 Page ID |
| `STATUS_PERIOD` | `1y` | 可用率统计周期：`24h`、`7d`、`30d`、`90d` 或 `1y` |
| `SCREENSHOT_TIMEOUT` | `90s` | 单次生成并回复状态图的总超时 |
| `SCREENSHOT_QUEUE_SIZE` | `3` | 等待生成的最大任务数；满载时让 QQ 官方重试事件 |
| `GGAPI_BALANCE_ENABLED` | `false` | 是否启用账号绑定与钱包余额查询 |
| `GGAPI_BASE_URL` | `https://www.ggapi.cc` | GGAPI 管理 API 的 HTTPS 地址 |
| `GGAPI_ADMIN_TOKEN` | 空 | GGAPI 只读管理令牌，启用时必填 |
| `GGAPI_SMTP_HOST` | 空 | 验证码邮件 SMTP 主机 |
| `GGAPI_SMTP_PORT` | `587` | SMTP 端口 |
| `GGAPI_SMTP_USERNAME` | 空 | SMTP 用户名 |
| `GGAPI_SMTP_PASSWORD` | 空 | SMTP 密码 |
| `GGAPI_SMTP_FROM` | 空 | 验证码邮件发件人 |
| `GGAPI_SMTP_FROM_NAME` | `GGAPI` | 验证码邮件发件人名称 |
| `GGAPI_SMTP_TLS_MODE` | `starttls` | `starttls` 或 `implicit_tls` |
| `HTTP_ADDR` | `0.0.0.0:8090` | HTTP 监听地址 |
| `DATA_PATH` | `/app/data/qq-status.json` | 配置、管理员哈希和收发摘要日志的本地文件路径 |
| `QQBOT_API_BASE_URL` | `https://api.bot.qq.com` | QQ 官方 API 地址，通常无需修改 |
| `QQBOT_TOKEN_URL` | `https://bots.qq.com/app/getAppAccessToken` | QQ 官方 Access Token 地址，通常无需修改 |

`QQBOT_ALLOWED_GROUPS` 使用官方事件中的 `group_openid`，不是数字 QQ 群号。首次接入可留空，从服务日志或调试事件确认 OpenID 后再收紧白名单。

## 状态图

状态图固定为 1280px 宽，高度随分组和卡片行数变化。它展示总览、统计周期、分组、当前状态、周期可用率、最近 100 次心跳、最新与平均延迟及平滑延迟曲线。状态映射如下：

| API 状态码 | 显示 | 颜色 |
|---|---|---|
| `1` | 在线 | 绿色 |
| `2` | 重试中 | 橙色 |
| `3` | 维护中 | 蓝色 |
| `0` | 离线 | 红色 |
| 其他或无心跳 | 未知 | 灰色 |

中文字体使用内嵌的 Noto Sans CJK SC，许可证保存在 `internal/statusimage/assets/OFL.txt`。运行容器不需要安装系统字体。

## 本地开发

```bash
cd frontend && pnpm install --frozen-lockfile && pnpm build && cd ..
go test ./...
go build ./...
docker compose config
docker compose build app
```

本地运行不需要浏览器服务：

```bash
QQBOT_APP_ID=... QQBOT_APP_SECRET=... go run .
```

## 安全边界

- 每个官方 Webhook 请求都使用 `AppSecret` 派生的 Ed25519 公钥验签。
- 回调请求体、签名、Access Token 和 AppSecret 不写日志。
- Access Token 仅缓存在内存中，过期前自动更新，收到 `401` 时刷新一次。
- 相同 `msg_id` 或互动事件 ID 保留 10 分钟去重，避免 QQ 官方重复投递导致重复执行。
- 状态与账号功能仅处理带机器人提及的群消息（兼容 `GROUP_AT_MESSAGE_CREATE` 和全量模式的 `GROUP_MESSAGE_CREATE`）；验证码摘要只在内存保存，10 分钟过期且最多尝试 5 次。SMTP 每成员及邮箱每小时最多发送 5 次，重发间隔 60 秒。
- GGAPI 管理端只调用 HTTPS GET；绑定查询会复核邮箱、角色和启用状态，身份变化时自动撤销本地绑定。
- 状态数据源只允许 HTTP/HTTPS，API 请求有固定超时和响应大小上限。
- 状态图预览必须经过管理端会话鉴权，并返回 `Cache-Control: no-store`。
