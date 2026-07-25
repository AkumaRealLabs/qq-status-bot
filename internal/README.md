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

1. **Domain 保持纯净。** 成本公式、分组匹配、密钥合并等规则都放这里。
2. **App 负责编排。** 加载规则 → 调 domain → 落库 / 调远端。
3. **Store 不做决策。** 优先接收 app 已合并好的实体。
4. **httpapi 保持薄。** Handler 只调 `s.App.*` 用例，禁止 `s.App.Store`。

## 演进路径

演进原则：先充实 domain（cost/merge）→ 再拆 app 子服务 → 按难测边界补 ports。

### Domain 纯规则面（已落地）

| 领域 | 包内文件 | 说明 |
|------|----------|------|
| 成本绑定 | `cost_binding.go` | 成本来源规范化、缺失原因 |
| 合并 / 密钥保留 | `merge.go`、`secrets.go` | 聚合上的 `MergeUpdate` |
| 通知映射 | `notify.go` | `ShouldNotify`、`AlertEventType` |
| 收入卡片规则 | `revenue.go` | 来源类型规范化 / 校验 |
| 调度分组匹配 | `scheduler_groups.go` | `GroupsForPrice`、`TargetGroups`、`SplitGroups` |

### App 门面（Phase 2）

`app.Service` 仍是 `httpapi` 的组合根。限界上下文以同包 facade 抽出：

| 门面 | 字段 | 职责 |
|------|------|------|
| `SchedulerService` | `Service.Scheduler` | 成本绑定、渠道成本分组/优先级或标签/权重同步 |

对外方法仍挂在 `*Service` 上，经 `facade_forwarders.go` 薄转发（避免改 HTTP handler）。

### 薄 httpapi 用例（Phase 3）

`use_cases.go` 暴露原先 handler 通过 `s.App.Store` 调用的持久化操作：

- 上游 get/delete + 浏览器 token 抓取
- 收入卡片 get/delete
- 运维事件 mark/ack（单条 + 批量）
- 审计写入、公开站点设置
- 导入/导出数据（`app.ExportData` DTO — httpapi 不必为它 import store）

**httpapi 已不再访问 `App.Store`。** 鉴权边界仍可在 `AuthenticatedRequest` 使用 `*store.Store` 查会话。

### 最小 ports（Phase 3 / PR7）

定义在 `ports.go`，挂在 `Service` 上（`New` 中默认注入）：

| 端口 | 字段 | 默认实现 | 使用方 |
|------|------|----------|--------|
| `Notifier` | `Notify` | Telegram（`sendTelegram`） | `alert`、`TestNotification` |
| `OneBotClient` | `OneBotClient` | OneBot HTTP 客户端 | 连接检查、通用文本发送 |

不是完整六边形重写：只对难测边界加端口。其余上下文仍用具体 `*store.Store` / `monitor.Client`。

### 后续可选

- 测试需要隔离时再加端口（upstream 仓库、余额 runner 等）
- 包体过大时再拆 facade 子包

## 已退役边界

- AUM 不提供公开状态页、模型探测、健康熔断/恢复或真实流量控制。
- `monitor` 仅负责上游余额、Key、订单与充值 API；不执行外部进程。
- 调度同步不得写渠道状态。GGAPI 仅写 `group/priority`，AxonHub 仅写托管标签与 `orderingWeight`。
- 旧表只存在于一次性兼容迁移 SQL；迁移标记为 `retire-monitoring-v1`，完成后不再创建。
- 利润快照、Telegram 用户会话/频道消息和 CLIProxy 号池管理已退役；Telegram Bot 通知仍通过 `Notifier` 保留。
