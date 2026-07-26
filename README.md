# AI Upstream Monitor

自用运维台：管理 `new-api` / `sub2api` 上游余额与成本，联动 GGAPI / AxonHub 成本字段、Telegram Bot 告警、通知规则与易支付收入。服务状态与模型可用性由 Uptime Kuma 负责。

## 快速启动

```bash
docker compose up -d --build
```

默认监听 `127.0.0.1:8090`。首次访问 `/admin` 创建管理员账号（密码至少 8 位）。`/`、`/admin` 与旧状态页路径都会跳转到 `/admin/balances`。

健康检查：`GET /api/health`

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `HTTP_ADDR` | `0.0.0.0:8090` | 监听地址 |
| `DATABASE_DSN` | `/app/data/monitor.sqlite` | SQLite 路径，或 `postgres://...` |
| `PB_DATA_DB` | 空 | 可选 PocketBase 旧库迁移路径 |
| `BROWSER_DEBUG_URL` | `http://127.0.0.1:19222` | 浏览器 CDP 调试地址 |
| `BROWSER_PROXY_URL` | 空 | noVNC 反代上游 |
| `BROWSER_VNC_URL` | 空 | 前端打开的 VNC 页面 |
| `TZ` | 系统时区 | 收入「今日」边界等使用 |

## 功能概览

- **余额监控**：上游额度、低余额告警、在线充值/兑换
- **成本管理**：上游 Key / 手动成本倍率、GGAPI / AxonHub 渠道绑定、成本档位、手动与自动成本同步
- **今日收入**：易支付 / new-api / sub2api 订单
- **事件 / 审计 / 通知规则 / 系统自检**

## 运维说明

### 数据与备份

- 业务库：`./data/monitor.sqlite`（SQLite 默认开启 WAL）
- 后台「设置 → 导出敏感备份」会包含上游密钥、Telegram Bot Token 与 OneBot Token，按机密文件保管
- 定时任务每小时清理过期时序数据（余额快照 30 天、审计 90 天等）

### 安全

- Session Cookie：`HttpOnly` + `SameSite=Lax`，HTTPS 下自动 `Secure`
- API 响应中的 token/password/key 已脱敏，仅返回 `*_set` 标志；编辑时留空表示不修改
- 登录失败限流；非 GET 请求校验 Origin
- 绑定 `127.0.0.1` 时仍建议放在受信任网络或反代后

### OneBot / LLBot 连接

- Compose 直接运行 LuckyLilliaBot 官方镜像，版本钉在 `linyuchen/llbot:8.0.14`（回调签名协议按该版本实现，跟 `latest` 会在上游改协议时静默打断 webhook 校验；需要临时换版本用 `LLBOT_IMAGE` 覆盖）。`3000` 只在 Compose 内网开放，WebUI 仅绑定 `127.0.0.1:3080`。
- 首次部署后通过 SSH 隧道访问本机 WebUI，完成 QQ 登录。首次登录会生成该 QQ 号的持久化配置，后续由 LLBot 的 `/app/llbot/data` 卷维护。
- 在 LLBot WebUI 的 OneBot 11 配置中，为 HTTP 与 HTTP POST 分别设置 Token；HTTP 监听 `0.0.0.0:3000`，HTTP POST 回调保持 `http://app:8090/api/onebot/events`，消息格式为数组。
- 在后台「设置 → OneBot 连接」填入 `http://llbot:3000`、HTTP Token、Webhook Token 和允许发送的 QQ 群号。AUM 保留连接检查、Webhook 签名校验与通用文本发送客户端，不解析或回复“状态/status”命令。
- LuckyLilliaBot `v8.0.14` 对 HTTP POST 使用 `X-Signature: sha1=<HMAC>` 校验回调 Token；本服务按该上游协议验证原始请求体，不保存或记录 Token、请求体。

### 成本同步

- 每轮余额与 Key 刷新完成后执行一次成本同步，也可在「成本管理」手动触发。
- GGAPI 只写渠道 `group` 与 `priority`；AxonHub 只写 AUM 托管标签与 `orderingWeight`。任何同步路径都不修改渠道启停状态。
- AUM 保存最近一次成本字段基线。检测到管理员或其他系统修改后会暂停自动覆盖，需在成本绑定上明确“重新接管”。
- 生产升级前必须备份 SQLite / Postgres 数据库或导出敏感备份。迁移会不可逆删除旧利润快照、Telegram 用户会话/频道消息、CLIProxy 配置与配额快照；升级后可删除旧 `/app/data/tg_media` 媒体和头像缓存。

### 前端入口

- `/` 与旧状态路径跳转 `/admin/balances`
- `/admin/costs` 是独立成本管理页；旧 `/admin/scheduler` 跳转该页

## 开发

```bash
# 后端
go test ./...

# 前端
cd frontend && pnpm install && pnpm dev
```

生产镜像多阶段构建：前端 Vite → Go embed `frontend/dist` → Alpine 运行时。
