# Stratum UI/UX 全面重构计划（Material You × FlClash 自研交互体系）

> 基准：Google Material You (Material Design 3 Expressive)、FlClash `0.9.x` 自研桌面交互与动态配色体系、Modern Web Guidance、Frontend Design。彻底移除全部 Apple Design / Liquid Glass / Safari / Finder / macOS 系统设置视觉与交互。

## 阶段一：协议扩展与本地化文案
- [x] `proto/proto.go`: 在 `Settings` 新增 `SeedColor string`、`ColorStyle string`（`soft | vivid | tinted | monochrome`）、`PureBlack bool`
- [x] `locales/`: 增补 Navigation Rail 目的地、色彩风格、矿物预设色、Pure Black、过滤 Chip、Chrome 浏览器 Omnibox 等中英文文案并重新生成 `catalog_gen.go`

## 阶段二：Go 原生 HCT 动态色彩引擎、M3 动效与主题底座
- [x] `app/hct.go`: 纯 Go 实现 CAM16/HCT 转换与 `TonalPalette`，复刻 FlClash 四种 `ColorStyle` 与 `PureBlack` 重映射（含 64 槽 LRU 缓存与 6 组矿物种子色，默认 Cobalt Basalt `#3B6698`）
- [x] `app/hct_test.go`: 校验 HCT 精度与全部预设 × 风格 × 浅/深/纯黑模式下的 WCAG 4.5:1 / 3:1 对比度
- [x] `app/motion.go`: 定义 M3 `ui.CubicBezier` 缓动曲线、Oklab 色彩动画令牌、`m3State` 三态叠加与 WCAG 3:1 `m3Focus` 焦点环
- [x] `app/theme.go` & `app/style.go`: 升级 `Palette` 为完整 M3 Tonal 语义色阶，彻底移除 `plugins/glass`、`rim`、`specular`、`sideTint`、`folderDot`；恢复 Markdown 代码块语义语法高亮
- [x] 删除 `app/haptic_darwin.go` 与 `app/haptic_other.go`，移除全部 `haptic` 调用链

## 阶段三：不透明外壳、M3 Navigation Rail 与会话列表面板
- [x] `cmd/stratum/main.go`: 移除 `VibrancyGlass` 与 Safari UA 后缀，设置 `TitleBarHeight: 48` 与 M3 `SurfaceContainer` 不透明初始底色
- [x] `app/rail.go`: 实现常驻左侧 `68dp` M3 Navigation Rail（FAB 新建会话、`⌘K` 搜索、`会话 / 自动化 / 扩展 / 设置` 胶囊指示器与连接状态）
- [x] `app/app.go`: 一级路由 `navPage` 替代全屏拦截，主舞台/辅助栏采用 `16dp` 圆角不透明层理容器，M3 `4×36dp` 胶囊分隔抓手取消拖拽过半隐式关闭
- [x] `app/sidebar.go` & `app/sessions.go`: 移除 `sideToggle` 悬浮药丸与底部渐变遮罩，增加 FlClash 栏内展开搜索与状态 `FilterChip`，会话行采用多线索状态标与 `FocusWithin` 显露操作

## 阶段四：主对话舞台、停靠式 Composer 与卡片重塑
- [x] `app/transcript.go` / `app/transcript_items.go` / `app/turns.go`: 结构化 `48dp` Top App Bar（移除滚动渐变遮罩），M3 非对称用户气泡，FlClash 风格 Tonal 折叠工作卡与紧凑统计胶囊
- [x] `app/composer.go` & `app/hello.go`: 底部停靠式 `24dp` `SurfaceContainerLow` 容器，M3 `InputChip` / `AssistChip` / Filled IconButton，Bento 工作区概览卡片
- [x] `app/approval.go` & `app/cards.go`: `SurfaceContainerHigh` + 左侧 `4dp` 语义色条审批卡与 M3 三级按钮；无阴影 Tonal 工具卡

## 阶段五：右侧辅助工作台、Chrome 风格浏览器与 Pierre WebView
- [x] `app/panes.go` / `app/review.go` / `app/filetree.go` / `app/terminal.go`: 移除 `glass.ScrollEdge` 与 iOS `rv-thumb` 滑块，改用带 `3dp` 动画指示条的 M3 Primary Tabs 与 FlClash 栏内搜索/过滤 Chip
- [x] `app/browser.go` & `app/webdata.go`: 常驻顶部标签条 + 左对齐完整 URL 胶囊形 Omnibox + `28dp` M3 确认弹窗（默认聚焦取消）
- [x] `app/pierre.go` & `app/web/`: 补齐 `color-scheme: light dark` 与完整 HCT CSS 变量同步，采用 Oklab 混色、现代字体栈与无过渡滚动条，重编 `app/web/dist/`

## 阶段六：一级功能页、SearchView 与文档同步
- [x] `app/settings.go` & `app/mcp.go`: 融入舞台的双栏 M3 设置中心、带 `✓` 的 M3 Segmented Button、FlClash 同款实时主题预览与矿物取色盘
- [x] `app/automations.go` & `app/login.go`: 迁移至舞台 Tonal 卡片布局与双栏引导卡
- [x] `app/palette.go` / `app/menus.go` / `app/mention.go` / `app/slash.go` / `app/rewind.go` / `app/find.go`: `28dp` M3 SearchView（带分类 `FilterChip` 与按下+释放外部关闭）及触发角展开菜单
- [x] `design/README.md`: 更新为 Material You × FlClash Tonal Strata 设计规范

## 阶段七：测试更新与全量验证
- [x] 更新 `app/ergonomics_test.go`、`app/browser_test.go`、`app/settings_test.go`、`app/fidelity_test.go`、`app/jolt_test.go` 等用例
- [x] 全量运行 `npm run build`、`go test ./...`、`go vet ./...`、`rg` 零残留检查及 `STRATUM_SNAPSHOTS` 渲染验证
