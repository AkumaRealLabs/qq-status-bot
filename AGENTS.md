# QQ 状态图机器人 - Agent 规则

面向在本仓库工作的编码助手。功能与部署说明见 `README.md`，后端分层见 `internal/README.md`。

## 语言

- 文档、代码注释和面向用户的说明使用中文。
- 标识符、包名、JSON 字段、环境变量与 git 历史消息保持英文风格。

## 项目边界

本项目负责接收 QQ 开放平台官方群消息 Webhook、读取状态页公开 JSON API、生成中文 PNG、回复群图片，并提供本地管理配置、摘要日志和状态图预览。

- 不引入 Chromium、CDP、OneBot、LLBot、Telegram、余额监控、成本同步或收入功能。
- 不引入数据库；运行状态保存在单个 JSON 文件中。
- 默认不改变 `/qqbot/events`、`/api/health` 和已有 QQ 配置环境变量名。

## 后端分层

| 包 | 职责 | 禁止 |
|---|---|---|
| `domain` | 纯类型、配置校验与命令匹配规则 | I/O、第三方依赖 |
| `app` | Webhook 用例、队列、去重与状态图回复编排 | HTTP handler 细节、QQ REST 细节 |
| `qqbot` | QQ 官方验签、Access Token、图片上传与消息 API | 状态查询业务判断 |
| `statusapi` | 状态页 JSON API 客户端 | QQ 协议、图片绘制 |
| `statusimage` | 固定宽度 PNG 绘制和 API/renderer 组合 | QQ 协议、配置持久化 |
| `httpapi` | HTTP 路由、鉴权和状态码适配 | 绕过 `app.Service` 写业务流程 |

## 安全

- 不记录或返回 `QQBOT_APP_SECRET`、Access Token、Webhook 签名和完整回调请求体。
- 必须保留 Ed25519 Webhook 验签，不能用来源 IP 或普通 Token 替代。
- 状态图数据源只允许 HTTP/HTTPS，保持 15 秒上游超时和 2 MiB 响应上限。
- `/api/status-preview` 必须鉴权并返回 `Cache-Control: no-store`。

## 构建与测试

```bash
go test ./...
go build ./...
cd frontend && pnpm build
docker compose config
docker compose build app
```

改 Go 后至少运行 `go test ./...`。改 renderer 后更新并检查 golden PNG，同时用真实状态 API 生成图片，确认中文、尺寸、状态色和曲线正确。

## 改动范围

- 只改任务相关文件，避免无关重构。
- 用户未明确要求时，不执行 `git push`、force push 或修改 git config。
- 仅在用户明确要求时创建 commit。
