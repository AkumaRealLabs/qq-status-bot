# 前端（React + TypeScript + Vite）

本仓库管理后台与公开状态页的前端。构建产物由后端 `//go:embed` 嵌入 `frontend/dist`。

## 入口与分包

| 路径 | 应用 | 说明 |
|------|------|------|
| `/` | `PublicApp` | 公开状态页，独立懒加载图 |
| `/admin/*` | `AdminApp` | 管理后台（登录后） |

状态页按 **public / admin / shared** 拆分，admin 专有编辑与拖拽排序不进公开路径。

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

- 开发时 Vite 代理 API 到后端（见 `vite.config.ts`）
- 生产镜像：多阶段构建先打前端，再编译 Go 并 embed 静态资源
