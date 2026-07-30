# QQ 状态截图机器人 — Agent 规则

面向在本仓库工作的编码助手。功能与部署说明见 `README.md`，后端分层见 `internal/README.md`。

## 语言

- 文档、代码注释和面向用户的说明使用中文。
- 标识符、包名、JSON 字段、环境变量与 git 历史消息保持英文风格。

## 项目边界

本项目仅负责：接收 QQ 开放平台官方群消息 Webhook、截图指定状态页区域、通过 QQ 官方 API 回复群图片。

- 不增加管理前端或数据库。
- 不重新引入 OneBot、LLBot、Telegram、余额监控、成本同步或收入功能。
- 默认不改变 `/qqbot/events`、`/api/health` 及现有环境变量名。

## 后端分层

| 包 | 职责 | 禁止 |
|---|---|---|
| `domain` | 纯类型与命令匹配规则 | I/O、第三方依赖 |
| `app` | Webhook 用例、队列、去重与截图回复编排 | HTTP handler 细节、QQ REST 细节 |
| `qqbot` | QQ 官方验签、Access Token、图片上传与消息 API | 状态查询业务判断 |
| `browsercdp` | Chromium CDP 页面截图 | QQ 协议与群策略 |
| `httpapi` | HTTP 路由和状态码适配 | 绕过 `app.Service` 写业务流程 |

## 安全

- 不记录或返回 `QQBOT_APP_SECRET`、Access Token、Webhook 签名和完整回调请求体。
- 必须保留 Ed25519 Webhook 验签，不能用来源 IP 或普通 Token 替代。
- Chromium CDP 不对宿主机或公网暴露。
- 只允许 `http/https` 状态页和预签名上传地址。

## 构建与测试

```bash
go test ./...
go build ./...
docker compose build
```

改 Go 后至少运行 `go test ./...`。改浏览器入口或截图逻辑后，还需用实际 Chromium 验证截图非空且区域正确。

## 改动范围

- 只改任务相关文件，避免无关重构。
- 用户未明确要求时，不执行 `git push`、force push 或修改 git config。
- 仅在用户明确要求时创建 commit。
