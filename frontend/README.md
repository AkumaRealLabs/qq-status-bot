# 前端（React + TypeScript + Vite）

本仓库管理后台前端。构建产物由后端 `//go:embed` 嵌入 `frontend/dist`。

## 入口与分包

| 路径 | 应用 | 说明 |
|------|------|------|
| `/` | 重定向 | 跳转 `/admin/balances` |
| `/admin/balances` | `AdminApp` | 默认后台入口 |
| `/admin/costs` | `AdminApp` | 成本绑定、连接配置、成本档位与同步日志 |
| `/admin/*` | `AdminApp` | 其他管理页面（登录后） |

`main.tsx` 直接挂 `AdminApp`（没有公开页，也没有额外的 lazy 入口层）；分包由各 feature 页面的
`React.lazy()` 驱动，入口 chunk 只含外壳与鉴权。

旧地址回落写在 `AdminApp.tsx` 的 `legacyPaths`：`/status`、`/admin/status` 跳余额页，
`/admin/scheduler` 跳成本管理页，其余未知路径一律回落余额页。服务端 `internal/httpapi/server.go`
有一张同名同内容的表（让直接输入旧地址时 URL 栏早一次往返就正确），**改动时两处要一起改**。
前端不再构建公开状态、健康控制、利润、Telegram 消息或号池管理 chunk。

## 字体（有意保留，勿当成待优化项）

`index.css` 全量 `@import` 霞鹜文楷 webfont：98 个 woff2 分片约 4.4MB，占 `dist` 的绝大头，
也占 Go 单二进制体积的 ~18%。这是**权衡后保留**的：

- 按 unicode block 裁剪只能省 0.02MB —— 4.4MB 基本全是 CJK 统一汉字，没有「不改观感又省体积」的方案。
- 浏览器靠 `unicode-range` 只下命中的分片（首屏约 300–900KB），文件名带 hash 走永久缓存。
- 自用工具 + 本地部署，18% 体积换整套视觉标识不值得。

要改只有两条路：整体移除改用系统 CJK 字体（中文观感明显变化），或按源码字符做静态子集
（动态内容如上游名、远端错误信息会混排两种字体，且要引入 Python/fonttools 构建依赖）。

共享能力集中在：

- `src/lib/` — 格式化、反馈文案、排序、工具函数
- `src/components/common.tsx` — 表格壳、表单页脚、行内消息等

## 常用命令

```bash
pnpm install
pnpm dev      # 本地开发
pnpm build    # 产出 dist，供 Docker / embed
pnpm preview  # 预览构建结果
```

Lint 使用 Oxlint（见 `.oxlintrc.json`）。

## 与后端的关系

- `vite.config.ts` 没有配 API 代理：`pnpm dev` 期间请求同源 `/api/*`，需要后端时自行加
  `server.proxy`，或直接访问后端 embed 的构建产物
- 生产镜像：多阶段构建先打前端，再编译 Go 并 embed 静态资源
