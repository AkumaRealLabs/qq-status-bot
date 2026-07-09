---
version: alpha
name: Claude-design-analysis
description: 暖色画布编辑风格界面（参考 Claude 产品视觉语言）。以浅奶油色画布为底，衬线展示标题、暖珊瑚色主按钮、深海军色产品表面（代码编辑器示意、模型卡片）。品牌张力来自奶油/珊瑚对比——刻意偏暖与人文，区别于多数 AI 产品的冷蓝+石板灰。字体：展示用板衬线（Copernicus / Tiempos Headline）作 h1/h2，正文人文字体；Anthropic 黑径向尖刺标志锚定 wordmark。本文件为设计令牌源，键名保持英文。

colors:
  primary: "#cc785c"
  primary-active: "#a9583e"
  primary-disabled: "#e6dfd8"
  ink: "#141413"
  body: "#3d3d3a"
  body-strong: "#252523"
  muted: "#6c6a64"
  muted-soft: "#8e8b82"
  hairline: "#e6dfd8"
  hairline-soft: "#ebe6df"
  canvas: "#faf9f5"
  surface-soft: "#f5f0e8"
  surface-card: "#efe9de"
  surface-cream-strong: "#e8e0d2"
  surface-dark: "#181715"
  surface-dark-elevated: "#252320"
  surface-dark-soft: "#1f1e1b"
  on-primary: "#ffffff"
  on-dark: "#faf9f5"
  on-dark-soft: "#a09d96"
  accent-teal: "#5db8a6"
  accent-amber: "#e8a55a"
  success: "#5db872"
  warning: "#d4a017"
  error: "#c64545"

typography:
  display-xl:
    fontFamily: "Copernicus, Tiempos Headline, serif"
    fontSize: 64px
    fontWeight: 400
    lineHeight: 1.05
    letterSpacing: -1.5px
  display-lg:
    fontFamily: "Copernicus, Tiempos Headline, serif"
    fontSize: 48px
    fontWeight: 400
    lineHeight: 1.1
    letterSpacing: -1px
  display-md:
    fontFamily: "Copernicus, Tiempos Headline, serif"
    fontSize: 36px
    fontWeight: 400
    lineHeight: 1.15
    letterSpacing: -0.5px
  display-sm:
    fontFamily: "Copernicus, Tiempos Headline, serif"
    fontSize: 28px
    fontWeight: 400
    lineHeight: 1.2
    letterSpacing: -0.3px
  title-lg:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 22px
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: 0
  title-md:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 18px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0
  title-sm:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 16px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0
  body-md:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: 0
  body-sm:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: 0
  caption:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 13px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0
  caption-uppercase:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 12px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 1.5px
  code:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: 0
  button:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1
    letterSpacing: 0
  nav-link:
    fontFamily: "StyreneB, Inter, sans-serif"
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  pill: 9999px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 48px
  section: 96px

components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 12px 20px
    height: 40px
  button-primary-active:
    backgroundColor: "{colors.primary-active}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.md}"
  button-primary-disabled:
    backgroundColor: "{colors.primary-disabled}"
    textColor: "{colors.muted}"
    rounded: "{rounded.md}"
  button-secondary:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 12px 20px
    height: 40px
  button-secondary-on-dark:
    backgroundColor: "{colors.surface-dark-elevated}"
    textColor: "{colors.on-dark}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 12px 20px
  button-text-link:
    backgroundColor: transparent
    textColor: "{colors.ink}"
    typography: "{typography.button}"
  button-icon-circular:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.full}"
    size: 36px
  text-link:
    backgroundColor: transparent
    textColor: "{colors.primary}"
    typography: "{typography.body-md}"
  top-nav:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.nav-link}"
    height: 64px
  hero-band:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.display-xl}"
    padding: 96px
  hero-illustration-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
  feature-card:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.title-md}"
    rounded: "{rounded.lg}"
    padding: 32px
  product-mockup-card-dark:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark}"
    typography: "{typography.title-md}"
    rounded: "{rounded.lg}"
    padding: 32px
  code-window-card:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark}"
    typography: "{typography.code}"
    rounded: "{rounded.lg}"
    padding: 24px
  model-comparison-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.title-md}"
    rounded: "{rounded.lg}"
    padding: 32px
  pricing-tier-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.title-lg}"
    rounded: "{rounded.lg}"
    padding: 32px
  pricing-tier-card-featured:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark}"
    typography: "{typography.title-lg}"
    rounded: "{rounded.lg}"
    padding: 32px
  callout-card-coral:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.title-md}"
    rounded: "{rounded.lg}"
    padding: 32px
  connector-tile:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.title-sm}"
    rounded: "{rounded.lg}"
    padding: 20px
  text-input:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: 10px 14px
    height: 40px
  text-input-focused:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
  cookie-consent-card:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.lg}"
    padding: 24px
  category-tab:
    backgroundColor: transparent
    textColor: "{colors.muted}"
    typography: "{typography.nav-link}"
    padding: 8px 14px
    rounded: "{rounded.md}"
  category-tab-active:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.nav-link}"
    rounded: "{rounded.md}"
  badge-pill:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    padding: 4px 12px
  badge-coral:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.caption-uppercase}"
    rounded: "{rounded.pill}"
    padding: 4px 12px
  cta-band-coral:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.display-sm}"
    rounded: "{rounded.lg}"
    padding: 64px
  cta-band-dark:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark}"
    typography: "{typography.display-sm}"
    rounded: "{rounded.lg}"
    padding: 64px
  footer:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark-soft}"
    typography: "{typography.body-sm}"
    padding: 64px
---

## 概览

本设计系统来自 Claude 产品营销站的暖色编辑风格，作为本仓库 UI 令牌参考。底色是**浅奶油画布**（`{colors.canvas}` — #faf9f5）——刻意偏暖，避免多数 AI 产品的冷灰白。标题使用**板衬线展示字体**（Copernicus / Tiempos Headline），字重 400 并带负字距；正文为 **StyreneB / Inter** 人文字体。整体更像文学刊物，而非典型 SaaS 营销页。

品牌张力来自**奶油 + 珊瑚**配对——珊瑚（`{colors.primary}` — #cc785c）是主强调色，用于主 CTA、字标点缀与整幅 callout 卡片。珊瑚偏暖、略闷，从不走青/蓝——有意区别于冷石板、饱和蓝与企业青。

系统有三种表面模式，在页面中交替出现：

1. **奶油画布**（`{colors.canvas}`）— 默认页面底
2. **浅奶油卡片**（`{colors.surface-card}`）— 功能卡片背景
3. **深海军产品面**（`{colors.surface-dark}`）— 代码编辑器示意、模型展示卡、页脚前 CTA、页脚本身

深色表面承载产品 chrome：代码块、终端输出、模型对比表、流程示意。奶油↔深色对比构成页面节奏。

**关键特征：**

- 暖奶油画布 + 暖深墨正文（`{colors.ink}` — #141413），这是品牌定调色
- 珊瑚主 CTA（`{colors.primary}` — #cc785c）：单按钮克制使用，整幅珊瑚 callout 可大胆铺满
- 板衬线展示标题（Copernicus / Tiempos Headline，字重 400 + 负字距）配人文无衬线正文
- 深海军产品示意卡（`{colors.surface-dark}` — #181715）展示代码/终端/模型数据，而非抽象营销插画
- 浅奶油功能卡（`{colors.surface-card}` — #efe9de）略深于画布，承载内容说明
- Anthropic 径向尖刺标志（四辐星号感）作字标前缀与内容标记
- 圆角分层：按钮/输入 `{rounded.md}`(8px)，内容/产品卡 `{rounded.lg}`(12px)，英雄插画 `{rounded.xl}`(16px)，徽章 `{rounded.pill}`
- 区块节奏 `{spacing.section}`(96px)；卡内内边距偏松 `{spacing.xl}`(32px)

## 颜色

### 品牌与强调

- **珊瑚 / 主色**（`{colors.primary}` — #cc785c）：主 CTA、整幅 callout、字标强调
- **珊瑚按下**（`{colors.primary-active}` — #a9583e）
- **珊瑚禁用**（`{colors.primary-disabled}` — #e6dfd8）
- **强调青绿**（`{colors.accent-teal}` — #5db8a6）：次要状态点，慎用
- **强调琥珀**（`{colors.accent-amber}` — #e8a55a）：分类徽章、行内高亮

### 表面

- **画布** `{colors.canvas}` #faf9f5：默认页面底，非纯白
- **柔表面** `{colors.surface-soft}` #f5f0e8：分区带
- **卡片表面** `{colors.surface-card}` #efe9de：功能/内容卡
- **强奶油** `{colors.surface-cream-strong}` #e8e0d2：选中 tab、强调分区
- **深表面** `{colors.surface-dark}` #181715：代码示意、页脚
- **深表面抬升** `{colors.surface-dark-elevated}` #252320：深色带内抬升卡
- **深表面柔** `{colors.surface-dark-soft}` #1f1e1b：深卡内代码块底
- **发丝线** `{colors.hairline}` #e6dfd8：奶油面上 1px 边
- **柔发丝线** `{colors.hairline-soft}` #ebe6df：同带内弱分隔

### 文字

- **墨** `{colors.ink}` #141413：标题与主文
- **正文强** `{colors.body-strong}` #252523
- **正文** `{colors.body}` #3d3d3a
- **次要** `{colors.muted}` #6c6a64
- **次要柔** `{colors.muted-soft}` #8e8b82
- **主色上** `{colors.on-primary}` #ffffff：珊瑚按钮字
- **深色上** `{colors.on-dark}` #faf9f5：深色面奶油白
- **深色上次要** `{colors.on-dark-soft}` #a09d96

### 语义

- **成功** `{colors.success}` #5db872
- **警告** `{colors.warning}` #d4a017
- **错误** `{colors.error}` #c64545

## 字体

### 字族

展示：**Copernicus**（或 **Tiempos Headline**）；正文/导航/标签：**StyreneB**（或 **Inter**）；代码：**JetBrains Mono**。

展示/正文分工：

- Copernicus 衬线（400、负字距）→ h1–h3、英雄展示
- StyreneB 无衬线（400–500）→ 正文、导航、按钮、说明
- JetBrains Mono → 代码与终端

### 层级

| Token | 字号 | 字重 | 行高 | 字距 | 用途 |
|---|---|---|---|---|---|
| `{typography.display-xl}` | 64px | 400 | 1.05 | -1.5px | 首页 h1 |
| `{typography.display-lg}` | 48px | 400 | 1.1 | -1px | 大区块标题 |
| `{typography.display-md}` | 36px | 400 | 1.15 | -0.5px | 次区块、模型名 |
| `{typography.display-sm}` | 28px | 400 | 1.2 | -0.3px | 定价档名、callout 标题 |
| `{typography.title-lg}` | 22px | 500 | 1.3 | 0 | 定价方案标签 |
| `{typography.title-md}` | 18px | 500 | 1.4 | 0 | 功能卡标题 |
| `{typography.title-sm}` | 16px | 500 | 1.4 | 0 | 连接器瓦片名 |
| `{typography.body-md}` | 16px | 400 | 1.55 | 0 | 默认正文 |
| `{typography.body-sm}` | 14px | 400 | 1.55 | 0 | 页脚细文 |
| `{typography.caption}` | 13px | 500 | 1.4 | 0 | 徽章、说明 |
| `{typography.caption-uppercase}` | 12px | 500 | 1.4 | 1.5px | 分类/NEW 标签 |
| `{typography.code}` | 14px | 400 | 1.6 | 0 | 代码 |
| `{typography.button}` | 14px | 500 | 1.0 | 0 | 按钮 |
| `{typography.nav-link}` | 14px | 500 | 1.4 | 0 | 顶栏菜单 |

### 原则

展示字重保持 400，勿加粗；负字距（-0.3～-1.5px）不可省。正文段落 400、标签 500；无衬线须人文（StyreneB/Inter），勿用几何无衬线作展示。

### 字体替代

若无 Copernicus/Tiempos：**Cormorant Garamond** 500 + -0.02em，或 **EB Garamond**。StyreneB 可用 **Inter**；有授权时 **Söhne** 亦可。

## 布局

### 间距

- **基单位：** 4px
- **Token：** xxs 4 · xs 8 · sm 12 · md 16 · lg 24 · xl 32 · xxl 48 · section 96
- **区块上下：** `{spacing.section}` 96px
- **卡内：** 功能/定价/对比卡 32px；代码窗/连接器瓦 24px
- **CTA 带：** 珊瑚 callout 内 48px；大深色 CTA 带 64px

### 栅格与容器

- 内容最大宽约 1200px 居中
- 英雄区常见 6/6 分栏
- 功能卡桌面 3 列、平板 2、手机 1
- 连接器瓦 4/6 列；定价桌面 3 列、手机 1 列

### 留白

奶油画布 + 衬线展示 + 宽松内边距形成编辑节奏；带间统一 96px，卡内 32px 让文字透气。

## 层级与深度

| 层级 | 处理 | 用途 |
|---|---|---|
| 平 | 无阴影无边 | 正文区、顶栏、英雄带 |
| 柔发丝 | 1px hairline | 输入、子导航、偶发卡边 |
| 奶油卡 | surface-card 底、无阴影 | 功能/内容卡 |
| 深表面卡 | surface-dark 底、无阴影 | 代码/模型示意 |
| 弱投影 | 低透明度阴影 | 极少用于抬升悬停 |

深度以**色块为主、阴影为辅**；多数纵深来自奶油↔深色对比。

### 装饰深度

- 尖刺标志作字标与行内标记
- 代码示意自带语法高亮、行号、状态条等内部层次
- 英雄插画多为珊瑚 + 深海军线稿，忌写实摄影

## 形状

| Token | 值 | 用途 |
|---|---|---|
| `{rounded.xs}` | 4px | 小徽章、小下拉 |
| `{rounded.sm}` | 6px | 小按钮、下拉项 |
| `{rounded.md}` | 8px | 标准 CTA、输入、分类 tab |
| `{rounded.lg}` | 12px | 内容/产品卡 |
| `{rounded.xl}` | 16px | 英雄插画容器 |
| `{rounded.pill}` | 9999px | 徽章胶囊 |
| `{rounded.full}` | 9999px | 圆形图标按钮、头像 |

摄影极少；以线稿、代码窗、终端、模型对比卡为主。头像若出现则圆形裁切约 40px。

## 组件

### 顶栏

**`top-nav`** — 奶油顶栏固定 64px，`{colors.canvas}` 底。左：尖刺标志 + 字标；中左：主导航；右：「登录」文字链 + 「试用」主按钮（珊瑚）。菜单字为 `{typography.nav-link}`。

### 按钮

**`button-primary`** — 珊瑚主 CTA：底 `{colors.primary}`，字 `{colors.on-primary}`，高 40px，圆角 md，内边距 12×20。按下变 `{colors.primary-active}`。

**`button-secondary`** — 奶油底 + 发丝描边，字 ink，尺寸同主按钮。

**`button-secondary-on-dark`** — 用于深表面卡上：底 surface-dark-elevated，字 on-dark；勿反成浅色次按钮。

**`button-text-link`** — 无底文字按钮（如顶栏登录）。

**`button-icon-circular`** — 36px 圆形图标钮，画布底 + 发丝边。

**`text-link`** — 正文内链用珊瑚主色；按下可下划线。

### 卡片与容器

**`hero-band`** — 奶油英雄 6-6 栅格：左标题/副文/按钮，右插画或产品卡；垂直 `{spacing.section}`。

**`hero-illustration-card`** — 英雄右侧大卡：线稿或深色代码窗，圆角 xl。

**`feature-card`** — 三列功能网格：surface-card 底，圆角 lg，内边距 xl；上图标 + title-md + body-md。

**`product-mockup-card-dark`** — 深海军产品 chrome 示意。

**`code-window-card`** — 深色代码窗（行号 + JetBrains Mono），圆角 lg，内边距 lg。

**`model-comparison-card`** — 模型对比：画布底 + 发丝边。

**`pricing-tier-card`** — 标准档位卡；**`pricing-tier-card-featured`** 用深表面反相标出主推档。

**`callout-card-coral`** — 整幅珊瑚 CTA；内可配奶油反色按钮。

**`connector-tile`** — 集成网格瓦片：画布 + 发丝边。

### 输入

**`text-input`** — 画布底、高 40px、圆角 md、发丝边。

**`text-input-focused`** — 边偏向珊瑚，外圈约 3px 15% 透明珊瑚环。

**`cookie-consent-card`** — 右下深色浮层 cookie 条。

### 标签

**`badge-pill`** / **`badge-coral`** — 分类胶囊与 NEW/BETA 珊瑚徽章。

### Tab

**`category-tab`** + **`category-tab-active`** — 未选 muted 字透明底；选中 surface-card + ink。

### CTA / 页脚

**`cta-band-coral`** — 页脚前整幅珊瑚 CTA。

**`cta-band-dark`** — 开发向页脚前深色 CTA，常配代码窗。

**`footer`** — 深海军页脚，多列链接；字标用 on-dark，永不反相为浅页脚。

## 建议与禁忌

### 建议

- 每页锚定奶油画布；纯白会像「任意 AI 工具」
- 展示标题一律衬线 400 + 负字距；正文无衬线
- 珊瑚留给主 CTA 与整幅 callout，勿到处涂珊瑚
- 用深色产品/代码卡展示真实 chrome，少用空泛插画
- 奶油功能卡与深海军示意卡交替成节奏
- 尖刺标志作字标前缀；字标内勿把标志反成白底
- 大区块间距用 `{spacing.section}` 96px

### 禁忌

- 勿用冷灰或纯白作画布
- 勿把衬线展示加粗到 700
- 勿用冷蓝/饱和青作品牌强调
- 勿满屏珊瑚；单元素克制、整幅 callout 才铺满
- 勿用无衬线做展示标题
- 勿连续两段同一表面模式
- 勿额外发明悬停花样：主色按下变深即可

## 响应式

| 名称 | 宽度 | 要点 |
|---|---|---|
| 手机 | < 768px | 汉堡导航；h1 缩小；英雄插画下叠；功能 1 列；定价 1 列；页脚单列 |
| 平板 | 768–1024px | 顶栏仍横排收紧；功能 2 列；连接器 3 列；定价 2 列 |
| 桌面 | 1024–1440px | 完整顶栏；功能 3 列；连接器 4/6 列；定价 3 列 |
| 宽屏 | > 1440px | 同桌面，外侧更松；内容最大宽约 1200px |

### 触控目标

- 主按钮至少 40×40
- 圆形图标钮 36×36
- 输入高 40px
- 连接器瓦整卡可点

### 折叠策略

- <768px 顶栏汉堡，全屏奶油菜单层
- 英雄 6-6 改单列：文案按钮在上、示意在下
- 网格减列而非缩卡
- 定价 4→2→1，主推深色档始终可辨
- 代码卡横向滚动保可读，勿硬折行

## 迭代指南

1. 一次只改一个组件，引用其 YAML 键
2. 变体（-active / -disabled / -focused）在 `components:` 中单独条目
3. 一律 `{token.refs}`，勿内联 hex
4. 不单独写 hover 文档；只记默认与按下
5. 展示衬线 400 + 负字距 / 正文无衬线 400 的分工不可破
6. 奶油 + 珊瑚 + 深海军为三元，勿加第四套表面色调
7. 需要强调时优先放大衬线字号，而非加粗

## 已知缺口

- Copernicus / StyreneB 为授权字体，公开页需用文档中的替代栈
- 径向尖刺标志作 SVG 资源，未形式化为 token
- 动画与过渡时长不在本文件范围
- 表单校验除 focused 外未完整抽取
- 产品聊天界面另有大量组件，超出本营销表面文档
- 部分「代理/控机」演示依赖动画，静态截图无法完整表达
