# 后端布局

该服务是一个使用 JSON 文件保存管理配置与摘要日志的模块化单体。

| 包 | 说明 |
|---|---|
| `config` | 从环境变量读取并校验运行配置 |
| `domain` | 群消息、管理配置与命令匹配纯规则 |
| `app` | 官方回调处理、快速 ACK、去重队列、状态图生成与回复编排 |
| `qqbot` | QQ 官方 Ed25519 验签、Access Token、分片上传与消息 API |
| `statusapi` | 并发读取状态页公开 JSON API，并限制协议、超时与响应大小 |
| `statusimage` | 使用内嵌中文字体和 Go 图形库生成 PNG |
| `store` | 原子持久化配置、管理员哈希、内存会话与摘要日志 |
| `httpapi` | Webhook、健康检查、管理 API 与已鉴权 PNG 预览 |

依赖方向以 `httpapi -> app -> domain` 为主。`app` 通过小型接口调用 `qqbot` 与 `statusimage`；`statusimage` 依赖 `statusapi` 获取结构化数据，HTTP 层不会绕过应用层直接生成预览。
