# QQ 状态截图机器人

一个只做一件事的 QQ 开放平台官方机器人：群成员 `@机器人 状态`（或 `status`）后，服务打开 [GGAPI 状态页](https://status.ggapi.cc)，截取指定区域，并通过官方群聊消息接口回复图片。

项目不再包含管理前端、数据库、上游余额、成本同步、收入、Telegram、OneBot 或 LLBot。

## 工作流程

```text
QQ 官方 Webhook
  -> Ed25519 验签并立即 ACK
  -> 单工作线程打开 Chromium
  -> 按 CSS selector 裁剪状态页
  -> 官方分片上传图片
  -> 使用原消息 msg_id 被动回复群图片
```

截图字节直接上传到 QQ 官方的预签名存储地址，不需要额外提供公网图片 URL。

## QQ 开放平台配置

1. 在 [QQ 开放平台](https://q.qq.com/) 创建机器人，取得 `AppID` 与 `AppSecret`。
2. 在事件订阅中启用 `GROUP_AT_MESSAGE_CREATE`。
3. 将 HTTPS 回调地址设为 `https://你的域名/qqbot/events`。
4. 按开放平台要求配置服务器出口 IP 白名单。
5. 将机器人添加到测试群；群里发送 `@机器人 状态`。

QQ 官方只允许 Webhook 使用 HTTPS，且回调端口必须是 `80`、`443`、`8080` 或 `8443`。Compose 默认仅在宿主机 `127.0.0.1:8090` 监听，请使用现有反向代理提供 HTTPS。

## 启动

```bash
cp .env.example .env
docker compose up -d --build
```

首次打开 `/admin/` 创建管理员账号；随后在「机器人配置」页填写 AppID、AppSecret、群白名单和截图参数。配置保存到 `./data/qq-status.json`，不会写入前端或日志。

健康检查：`GET /api/health`

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `QQBOT_APP_ID` | 必填 | QQ 开放平台机器人 AppID |
| `QQBOT_APP_SECRET` | 必填 | QQ 开放平台机器人 AppSecret；禁止提交到仓库 |
| `QQBOT_ALLOWED_GROUPS` | 空 | 允许查询的群 OpenID，逗号分隔；空表示全部已授权群 |
| `STATUS_COMMANDS` | `状态,status` | 命令列表，逗号分隔，完整匹配且英文不区分大小写 |
| `STATUS_URL` | `https://status.ggapi.cc` | 要打开的状态页 |
| `SCREENSHOT_SELECTOR` | `main > div:not(.sticky)` | 截图区域 CSS selector |
| `SCREENSHOT_WIDTH` | `1280` | 浏览器视口宽度，范围 640–1920 |
| `SCREENSHOT_HEIGHT` | `900` | 浏览器视口高度，范围 480–2160 |
| `SCREENSHOT_WAIT` | `5s` | 页面完成加载后的额外等待时间 |
| `SCREENSHOT_TIMEOUT` | `90s` | 单次截图和回复总超时 |
| `SCREENSHOT_QUEUE_SIZE` | `3` | 等待截图的最大任务数；满载时让 QQ 官方重试事件 |
| `HTTP_ADDR` | `0.0.0.0:8090` | HTTP 监听地址 |
| `DATA_PATH` | `/app/data/qq-status.json` | 配置、管理员哈希和收发摘要日志的本地文件路径 |
| `BROWSER_DEBUG_URL` | `http://127.0.0.1:9222` | Chromium CDP 地址；Compose 内自动设为 `http://browser:9222` |
| `QQBOT_API_BASE_URL` | `https://api.bot.qq.com` | QQ 官方 API 地址，通常无需修改 |
| `QQBOT_TOKEN_URL` | `https://bots.qq.com/app/getAppAccessToken` | QQ 官方 Access Token 地址，通常无需修改 |

`QQBOT_ALLOWED_GROUPS` 使用官方事件中的 `group_openid`，不是数字 QQ 群号。首次接入可留空，从服务日志或调试事件确认 OpenID 后再收紧白名单。

## 截图区域

默认 selector 会排除状态页导航、刷新栏和页脚，只保留绿色状态总览与服务卡片。目标站改版后可直接调整 `SCREENSHOT_SELECTOR`，无需重新构建镜像。例如截图整个主体：

```dotenv
SCREENSHOT_SELECTOR=main
```

## 本地开发

```bash
go test ./...
go build ./...
```

本地运行需要一个开放 CDP 的 Chromium：

```bash
docker compose up -d browser
QQBOT_APP_ID=... QQBOT_APP_SECRET=... BROWSER_DEBUG_URL=http://127.0.0.1:9222 go run .
```

## 安全边界

- 每个官方 Webhook 请求都使用 `AppSecret` 派生的 Ed25519 公钥验签。
- 回调请求体、签名、Access Token 和 AppSecret 不写日志。
- Access Token 仅缓存在内存中，过期前自动更新，收到 `401` 时刷新一次。
- 相同 `msg_id` 保留 10 分钟去重，避免 QQ 官方重复投递导致重复截图。
- Chromium 仅在 Compose 内网开放 CDP，不映射到宿主机。
