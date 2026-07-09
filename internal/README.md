# 后端布局（演进式模块化单体 / DDD 方向）

## 分层

| 包 | 职责 |
|----|------|
| `domain` | 纯类型 + 业务规则（无 I/O，仅标准库） |
| `app` | 用例 / 编排（数据库 + HTTP 客户端 + 定时任务） |
| `store` | 仅持久化 — 长期不做密钥合并策略 |
| `monitor` / `epay` | 外部 API 的防腐层（ACL） |
| `httpapi` | HTTP 适配器 — 只调 `app`，不直连 `store` |

## 经验法则

1. **Domain 保持纯净。** 阈值、静音/恢复、密钥合并、公式都放这里。
2. **App 负责编排。** 加载规则 → 调 domain → 落库 / 调远端。
3. **Store 不做决策。** 优先接收 app 已合并好的实体。
4. **httpapi 保持薄。** Handler 只调 `s.App.*` 用例，禁止 `s.App.Store`。

## 演进路径

会话 DDD 计划：先充实 domain（probe/merge/profit）→ 再拆 app 子服务 → 再 ports + 去掉 Store 旁路。

### Domain 纯规则面（已落地）

| 领域 | 包内文件 | 说明 |
|------|----------|------|
| 探测静音 / 自动关渠 | `probe.go` | 阈值、恢复资格 |
| 合并 / 密钥保留 | `merge.go`、`secrets.go` | 聚合上的 `MergeUpdate` |
| 利润计算 | `profit.go` | `UsageUnits`、`LineProfit`、成本辅助 |
| 通知映射 | `notify.go` | `ShouldNotify`、`AlertEventType` |
| 收入卡片规则 | `revenue.go` | 来源类型规范化 / 校验 |
| 调度分组匹配 | `scheduler_groups.go` | `GroupsForPrice`、`TargetGroups`、`SplitGroups` |

### App 门面（Phase 2）

`app.Service` 仍是 `httpapi` 的组合根。限界上下文以同包 facade 抽出：

| 门面 | 字段 | 职责 |
|------|------|------|
| `SchedulerService` | `Service.Scheduler` | 配置、渠道/分组应用、成本快照、自动关渠/恢复 |
| `ProfitService` | `Service.ProfitSvc` | 基于调度日志的号池利润汇总 |
| `ProbeService` | `Service.Probe` | 模型卡片 CRUD、探测、上游检查、监控状态 |
| `CLIProxyService` | `Service.CLIProxy` | CLIProxyAPI 配置、鉴权文件、配额重置/快照 |
| `TGService` | `Service.TG` | Telegram 会话、频道、消息同步/媒体缓存 |

对外方法仍挂在 `*Service` 上，经 `facade_forwarders.go` 薄转发（避免改 HTTP handler）。

### 薄 httpapi 用例（Phase 3）

`use_cases.go` 暴露原先 handler 通过 `s.App.Store` 调用的持久化操作：

- 上游 get/delete + 浏览器 token 抓取
- 卡片 / 收入卡片 get/delete
- 运维事件 mark/ack（单条 + 批量）
- 审计写入、公开站点设置
- 导入/导出数据（`app.ExportData` DTO — httpapi 不必为它 import store）
- TG 频道/消息 list/get/delete

**httpapi 已不再访问 `App.Store`。** 鉴权边界仍可在 `AuthenticatedRequest` 使用 `*store.Store` 查会话。

### 最小 ports（Phase 3 / PR7）

定义在 `ports.go`，挂在 `Service` 上（`New` 中默认注入）：

| 端口 | 字段 | 默认实现 | 使用方 |
|------|------|----------|--------|
| `CardRepository` | `Cards` | `*store.Store` | ProbeService、SchedulerService、`GetCard` |
| `Notifier` | `Notify` | Telegram（`sendTelegram`） | `alert`、`TestNotification` |
| `ProbeRunner` | `Prober` | 实时 `monitor.Client.Probe` | 卡片探测路径 |

不是完整六边形重写：只对难测边界加端口。其余上下文仍用具体 `*store.Store` / `monitor.Client`。

### 后续可选

- 测试需要隔离时再加端口（upstream 仓库、余额 runner 等）
- 包体过大时再拆 facade 子包
- `ModelCard` 概念拆分（探测 vs 号池绑定）— 需要单独迁移设计
