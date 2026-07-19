# AI Upstream Monitor

自用运维台：监控 `new-api` / `sub2api` 上游余额与 `gpt-5.6-sol` 探测状态，联动调度器分组、利润核算、Telegram 消息、CLIProxy 号池与易支付收入。

## 快速启动

```bash
docker compose up -d --build
```

默认监听 `127.0.0.1:8090`。首次访问 `/admin` 创建管理员账号（密码至少 8 位）。

公开状态页：`/`  
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
| `TG_MEDIA_DIR` | `/app/data/tg_media` | Telegram 媒体缓存目录 |
| `AUM_PROBE_MODE` | `cli` | 探测模式：`cli`（Codex CLI）或 `http` |
| `TZ` | 系统时区 | 收入「今日」边界等使用 |

## 功能概览

- **状态监控**：模型卡片探测历史、公开页、失败静音
- **余额监控**：上游额度、低余额告警、在线充值/兑换
- **今日收入**：易支付 / new-api / sub2api 订单
- **调度器**：渠道绑定、成本分组与优先级调度、成本/售价快照与利润
- **号池**：CLIProxyAPI Codex 账号与配额（忽略 xAI）
- **最新消息**：Telegram 频道同步
- **事件 / 审计 / 通知规则 / 系统自检**

## 运维说明

### 数据与备份

- 业务库：`./data/monitor.sqlite`（SQLite 默认开启 WAL）
- 后台「设置 → 导出敏感备份」会包含密钥与 TG 会话，按机密文件保管
- 定时任务每小时清理过期时序数据（探测 14 天、余额快照 30 天、审计 90 天等）

### 安全

- Session Cookie：`HttpOnly` + `SameSite=Lax`，HTTPS 下自动 `Secure`
- API 响应中的 token/password/key 已脱敏，仅返回 `*_set` 标志；编辑时留空表示不修改
- 登录失败限流；非 GET 请求校验 Origin
- 绑定 `127.0.0.1` 时仍建议放在受信任网络或反代后

### 探测

- 默认用镜像内 `@openai/codex` CLI 探测；可设 `AUM_PROBE_MODE=http` 走 HTTP
- 定时检查对上游与卡片使用有限并发（3），避免串行拖超时
- 失败策略可在「通知规则」配置：告警阈值、静默/自动关渠阈值、本地错误内部重试次数与间隔

### 前端分包

- `/` 走 `PublicApp`，`/admin/*` 走 `AdminApp`（各自 lazy 加载）
- 状态页按 public / admin / shared 拆分，admin 专有编辑与 dnd 不进公开路径

## 开发

```bash
# 后端
go test ./...

# 前端
cd frontend && pnpm install && pnpm dev
```

生产镜像多阶段构建：前端 Vite → Go embed `frontend/dist` → Alpine 运行时。
