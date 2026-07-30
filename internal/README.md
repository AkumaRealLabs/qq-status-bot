# 后端布局

该服务是一个无数据库的最小模块化单体。

| 包 | 说明 |
|---|---|
| `config` | 从环境变量读取并校验运行配置 |
| `domain` | 群消息类型与命令匹配纯规则 |
| `app` | 官方回调处理、快速 ACK、去重队列、截图与回复编排 |
| `qqbot` | QQ 官方 Ed25519 验签、Access Token、分片上传与消息 API |
| `browsercdp` | 创建临时 Chromium 页面并按 selector 裁剪 PNG |
| `httpapi` | `/qqbot/events` 和 `/api/health` HTTP 适配器 |

依赖方向：`httpapi -> app -> domain`。`app` 通过小型接口调用 `qqbot` 与 `browsercdp`，便于在测试中隔离外部边界。
