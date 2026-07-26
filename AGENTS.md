# AI Upstream Monitor — Agent 规则

面向在本仓库工作的编码助手。细则见 `README.md`、`internal/README.md`、`frontend/README.md`、`DESIGN.md`；本文件只写**必须遵守的约定**。

## 语言

- **文档、代码注释、面向用户的说明**：中文。
- **标识符、包名、JSON 字段、环境变量、git 历史消息**：保持现有英文风格；不要为「全中文」去改 API/字段名。
- 专有名词可中英并存，例如：`facade（门面）`、`port（端口）`、`MergeUpdate`。

## 项目是什么

自用运维台：监控 new-api / sub2api 上游余额，联动 GGAPI / AxonHub 成本字段、Telegram Bot 告警、通知规则与易支付收入。

- 后端：Go 单二进制，`frontend/dist` 由 `//go:embed` 嵌入。
- 前端：React + TypeScript + Vite + Tailwind；pnpm。
- 数据：默认 SQLite（`DATABASE_DSN`），可选 Postgres；导出备份含密钥，按机密处理。

## 后端架构（必须遵守）

分层与演进说明：`internal/README.md`。

| 包 | 职责 | 禁止 |
|----|------|------|
| `domain` | 纯类型 + 业务规则 | I/O、第三方依赖（仅 stdlib） |
| `app` | 用例编排、facade、ports | 把公式/阈值策略堆回 handler |
| `store` | 持久化 | 业务决策、长期依赖的 secret merge 策略 |
| `monitor` / `epay` | 外部 API 防腐层 | 泄漏到 httpapi 业务分支 |
| `httpapi` | HTTP 适配器 | `s.App.Store.*`；写业务状态机 |

硬性规则：

1. **httpapi 只调 `s.App.*`**，不要重新引入 Store 旁路。鉴权边界可在 `AuthenticatedRequest` 用 `*store.Store` 查会话。
2. **密钥/空字段**：编辑时留空 = 不修改；合并走 domain `MergeUpdate` / `KeepSecret`，app 编排后落库。
3. **facade**：`Scheduler` / `OneBot`；对外仍挂在 `*Service`，经 `facade_forwarders.go` 转发，避免无谓改 handler。
4. **ports**（`Cards` / `Notify` / `Prober`）：只在难测边界扩展；不要一次铺满六边形。
5. **导出 DTO**：对 httpapi 使用 `app.ExportData`，不要让 handler 为备份类型 import `store`。
6. **默认不改** REST 路径与 JSON 字段名；不拆微服务、不换库、不重写 PocketBase 迁移，除非用户明确要求。

## 前端约定

- 只有一个应用：`main.tsx` 直接挂 `AdminApp`（不要再包一层 lazy 入口，公开页已删除）。
- 分包按标签页做：`AdminApp` 里各 feature 页面用 `React.lazy()`，入口 chunk 只装外壳与鉴权。
- 旧地址回落表两处要同步：`AdminApp.tsx` 的 `legacyPaths` 与 `internal/httpapi/server.go` 的 `legacyPaths`。
- 共享：`src/lib/*`、`src/components/common.tsx`；优先复用反馈/表格/格式化，避免复制粘贴。
- 样式：Tailwind + 现有组件模式；设计令牌参考 `DESIGN.md`（键名英文）。
- 包管理：`pnpm`（不要擅自改成 npm/yarn）。

## 构建与测试

```bash
# 后端（改 Go 后必跑；main 包 embed frontend/dist，需先有 dist 或先 pnpm build）
go test ./...

# 前端
cd frontend && pnpm install
pnpm dev          # 开发
pnpm build        # tsc -b && vite build
pnpm lint         # oxlint
```

- 生产：`docker compose up -d --build`；多阶段 Dockerfile（前端 build → Go embed → 运行时）。
- 只改注释/文档时可不强制前端 build；改 TS/组件逻辑后至少 `pnpm build` 或等价类型检查。
- 不要提交密钥、`.env` 真值、导出的敏感备份、本地 `data/*.sqlite` 业务库。

### CI（GitHub Actions）

| 工作流 | 触发 | 做什么 |
|--------|------|--------|
| `.github/workflows/ci.yml` | PR + push `main`（忽略纯 md） | pnpm build/lint + `go test ./...` |
| `.github/workflows/docker.yml` | push `main` / tag `v*`（忽略纯 md） | 仅 buildx 推 GHCR；**不**在 host 再跑前端/单测 |

- Docker 内自己构建前端，避免与 CI 双构建。
- browser 镜像仅在 `browser/**` 或该 workflow 变更、或打 tag 时构建。
- 推送 GHCR 使用 `secrets.GHCR_TOKEN` + `github.repository_owner`。

## 安全与数据

- API 响应中 token/password/key 脱敏，只暴露 `*_set`；保存时空串表示不改。
- Session：`HttpOnly` + `SameSite=Lax`；勿削弱 Origin 校验与登录限流。
- 备份接口含密钥与 TG 会话：文档与 UI 文案保持「敏感」提示，不要当普通导出。

## 改动范围

- 只改任务相关文件；禁止顺手大重构、扩 scope 的「顺便清理」。
- 新业务规则优先进 `domain` + 表驱动测试，再在 `app` 接线。
- 用户未明确要求时：**不要** `git push`、不要 force push、不要改 git config。
- 提交：完整句子的中文或英文 message 均可，说明「为什么」；仅在用户要求时 commit。

## 常用路径

| 路径 | 内容 |
|------|------|
| `internal/domain/` | 纯规则 |
| `internal/app/` | 编排、facade、ports、use_cases |
| `internal/store/` | SQL 持久化 |
| `internal/httpapi/` | 路由与 handler |
| `internal/monitor/`、`internal/epay/` | 上游/易支付客户端 |
| `frontend/src/` | UI |
| `main.go` | 进程入口与 embed |
| `docker-compose.yml` | app + browser 侧车 |
