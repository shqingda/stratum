# 交接给 Antigravity（2026-11-24，`main` = `ac89ae7`）

先读完「项目」「规则」「mygo 的坑」三节再动手。「待办」是要做的事。「历史记录」只在需要查某个功能为什么这样做时再看。

## 项目

- **是什么**：Stratum，macOS 上的 coding agent 桌面应用，对标 Claude Code 桌面版和 Codex 桌面版。全部用 Go 写。
- **目录**：`/Users/shqingda/Projects/stratum`。远端 `https://github.com/shqingda/stratum.git`，只有 `main` 分支。
- **界面框架**：`github.com/egoist/mygo` v0.10.0 的 `ui` 包，是立即模式（每帧重画）的原生界面工具包，不是网页。源码在 `~/go/pkg/mod/github.com/egoist/mygo@v0.10.0`，接口不清楚时直接读源码。
- **进程**：界面和内核在同一个进程（`cmd/stratum` 调 `kernel/boot.Start`）。`cmd/stratumd` 是没有界面的内核。
- **目录分工**：
  - `app/`：界面。`app/web/` 是 diff 和文件树用的 WebView 前端（esbuild 打包，`dist/` 已提交）。
  - `client/`：界面这一侧的状态和对内核的调用。
  - `proto/`：界面和内核之间的协议类型。
  - `kernel/`：代理运行时。`session`（对话轮次、审批）、`tools`（代理工具）、`perm`（权限判定）、`agents`（自定义代理）、`computer`（电脑操控）、`preview`（面板浏览器驱动）、`sandbox`、`skills`、`mcp`、`store`（SQLite）、`workspace`、`lsp`、`changes`、`rpc`。
  - `llm/`：各模型服务商的适配。`llm/testdata/` 是录制的真实响应，不要改。
  - `locales/`：中英文案，由 `scripts/locales.py` 生成。
  - `design/`：设计规范（`design/README.md`）、高保真样稿 `mockups.html`、主流 agent 调研（`design/research/README.md`）。
- **语言**：界面默认简体中文（`mygo.json` 的 `i18n.source` 是 `zh-Hans`），英文跟着加。

## 常用命令

- 看真机效果：用户自己在终端跑 `./scripts/demo.sh`。它用一个假的脚本账号和临时数据目录，不碰真实账号和钥匙串。你不要替用户跑它，也不要截用户桌面。
- 提交前必须全部通过：
  - `gofmt -l app kernel cmd proto locales llm client`（输出为空）
  - `go vet ./...`
  - `go tool mygo vet .`
  - `go build ./...`
  - `GOOS=linux go build ./kernel/... ./cmd/stratumd`
  - `go test ./...`
  - 改了 `app/web/` 时：`cd app/web && npm run build && npm run typecheck`
- 改界面文案：只改 `scripts/locales.py` 里的 `m("key", "中文", "English")`，然后跑 `python3 scripts/locales.py && go tool mygo generate`。不要手改 `locales/*.json` 和 `catalog_gen.go`。
- 界面测试用 `ui.NewTester(a.View, 宽, 高)` 离屏渲染；`tt.Image()` 可以存 PNG 自己看效果。这不算截用户桌面，可以用。

## 规则（用户定的，照做）

- **回复用简体中文**。结论放第一句，一句只说一件事。状态只用这些词：已完成、部分完成、未开始、进行中、已验证、未验证、失败、跳过、阻塞、未确认。没验证的不要说成好了。
- **交互不要自己发明**。先照 Apple 自家 App（Finder、Mail、Notes、Xcode、Safari、Spotlight、系统设置）和 Craft；Apple 没有对应做法时，再照 Codex、Claude Code、Claude 桌面版、versta、opencode、Antigravity。用户会问「主流 agent 怎么做的」，回答要有依据。
- **不准用用户的真实账号测试**。只用假实现和演示环境（`scripts/demo.sh`、`STRATUM_DEMO`）。
- **不准操作用户的桌面**：测试里不发鼠标键盘事件，不截真实屏幕，不碰剪贴板。电脑操控只用假桌面（`computer.Use`）测。
- **下载文件先问用户**（升级依赖除外：用户要求升级时可以下载）。
- **提交**：用户说「提交」或「推送」才提交，直接推 `main`。`git add` 只写具体路径，不要 `git add -A`。不提交 `tasks/`、`design/fidelity/out/`、`screenshot.png`。提交信息用英文，不要写 Claude 的署名行。
- **只做用户要的事**。用户说「不要干别的」时，连顺手修也不做，发现的问题写进回复。
- **依赖**：这些不要升，原因见下：`modernc.org/libc`（必须和 `modernc.org/sqlite` 配套）、`marked`（15→18 跨大版本）、`typescript`（5.9→7.0 换了编译器）、`esbuild`（0.x 每个小版本都可能不兼容）。
- `tasks/handoff.md`（本文件）早年被提交过一次，之后的改动都不提交。做完一段工作，把结果追加到本文件。

## mygo 的坑（踩过的）

- 元素身份按在父元素里的位置算。前面有元素时出时不出，后面的元素会被认错、状态和尺寸会跳。给这类元素加 `c.Key("名字")`。
- `Element.Bounds()` 是上一帧的布局，第一帧是 0。
- `ui.Tester.Frame()` 会连跑最多 20 帧直到稳定，单帧的跳动它看不出来。查抖动用 `tt.Advance(8*time.Millisecond)` 逐帧推进并比较像素（例：`app/jolt_test.go`）。
- `TextInputBase(...).OnTextKey(...)` 的回调在两帧之间跑，结果要存在 `App` 字段上，不能存在闭包的局部变量里。
- `ui.CheckboxBase` 什么都不画，要自己画勾选框。
- `mygo.Dialog.Save` 会把任务投到主线程再等它，不能在主线程上同步调用（会卡死），要放进 goroutine。
- 设置页打开指定分页：先 `openSettings()`，再设 `settingsPage`；`openSettingsPage` 不会打开设置窗口。
- JXA（`osascript -l JavaScript`）里 `NSArray.count` 返回的是字符串，判断要写 `count > 0`。
- 改了 mygo 窗口或全局快捷键相关代码，不要在持有自己的锁时调 mygo 窗口方法：它们会等主线程，主线程的回调又可能要同一把锁，会死锁（例：`app/control.go` 用两把锁）。
- `go tool mygo dev`（`./scripts/demo.sh`）默认设置 `CGO_ENABLED=0`，不能在 `app/` 里写 `import "C"`（cgo）；调 macOS 原生 API 应使用 `github.com/ebitengine/purego/objc`。

## 当前状态

- `main` 最新 `0fc8e88`（已推送，包含以下 5 项全量合并提交；工作区仅保留未跟踪的 `tasks/` 与 `.claude/`）：
  - 阶段一：统一三栏顶栏高度为 `52pt`；保留 `sideToggle` 悬浮胶囊与 `74pt` 收起间距、`workspaceCapsule` `p.Chip` 胶囊与 `rim` 描边，`sideTint` 提高至深色 `.82` / 浅色 `.80` 以减弱左侧栏 Liquid Glass 透出；干净时的 `reviewToggle` 改为无边框工具栏控件；侧栏校准行高与字阶（导航 32pt/14pt，项目组 28pt/13.5pt，会话 32pt/14pt）、去浅色卡片阴影与常驻时间戳、待审批保留单处蓝色信号；清理全仓 56 处死 `press()` 调用。
  - 阶段二：审批卡去除蓝色全框与 `waited()` 1Hz 计时跳动、去除重复「当前模式」行；历史回复操作栏与时间戳改悬停淡入；思考/工作流/通知展开改纯淡入（120ms）；单轮改动条瘦身为 `Height(34)`；空会话概览加宽至 `480px`、放大字号/行高并去掉冗余对勾。
  - 阶段三：输入框统一 `16px` 同心圆角与防跳动 `InsetShadow` 聚焦边；幽灵建议 `Tab` 键帽内联至文本区右端防止底栏位移；去掉 `⌘K` 脚栏与 `/btw` 头栏冗余徽章。
  - 阶段四：无计划时动态隐藏「计划」页签；右栏分段滑块去多层叠加；设置页去掉顶部冗余 `lede` 解释段。
  - 阶段五：WebView（`app/web/` + `app/pierre.go`）加入 `color-scheme` meta、`accent-color`、`::selection`、`scrollbar-color`（含 WebKit 回退）、`:focus-visible`、原生 `<dialog>` 评论框与简中/英本地化下发。
  - 性能优化（1～4 项，Apple M3 Pro `-benchtime=500ms` 实测前后对比）：
    - 项 1 Review Diff（`app/review.go`、`app/remarks.go`）：`fileDiff` 增加 `sameDiffSource` 指针与长度 O(1) 校验、`diffHash` 改用 `io.WriteString`、缓存主题 token 样式与行内 `[]ui.Span`、空闲跳过 `remarksAt` 与 `at.at()`、`diffLineKey`/`strconv.Itoa` 替 `fmt.Sprint*`；`BenchmarkFrameReviewDiff` 从 `17.25 ms / 20.71 MB / 16,932 allocs` 降至 `11.09 ms / 5.28 MB / 8,663 allocs`（耗时 -35.7%，内存 -74.5%，分配 -48.8%）。
    - 项 2 20k 行 Diff（`app/diff.go`、`app/review.go`）：`myers`/`backtrack` 改用分块 `int32` frontier（仅存 `d` 个活跃项 + 64 KiB 定长块消除切片扩容拷贝）与原地 `slices.Reverse`，`tokenizeNeeded` 仅扫到最后一个 hunk 并分块打包保留 token；`BenchmarkNewFileDiff` 从 `74.89 ms / 309.1 MB / 244,420 allocs` 降至 `56.75 ms / 47.7 MB / 4,955 allocs`（耗时 -24.2%，内存 -84.6%，分配 -98.0%）。
    - 项 3 长对话流（`app/app.go`、`app/transcript.go`、`app/turns.go`）：复用 `itemsBuf`、`row` 改标量返回、`finished` 原地追加、无审批时延迟分配 `asked` map；`BenchmarkFrameTranscript` 从 `0.335 ms / 810 KB / 2,290 allocs` 降至 `0.230 ms / 8.2 KB / 250 allocs`（耗时 -31.3%，内存 -99.0%，分配 -89.1%）。
    - 项 4 流式与侧栏（`client/client.go`、`client/stream_bench_test.go`、`app/sidebar.go`、`app/app.go`）：`appendDeltaText` 纯 JSON 快路径免去 `json.Unmarshal` 反射，复用 `sideGroups` 且 `ago(...)` 仅悬停计算；`BenchmarkStreamDeltas` 从 `2.29 ms / 1.70 MB / 10,040 allocs` 降至 `0.438 ms / 1.54 MB / 37 allocs`（耗时 -80.9%，分配 -99.6%），`BenchmarkFrameSidebar` 从 `0.378 ms / 211 KB / 831 allocs` 降至 `0.319 ms / 14.2 KB / 666 allocs`（内存 -93.3%），`BenchmarkFrameApproval` 从 `0.194 ms / 14.6 KB / 247 allocs` 降至 `0.194 ms / 8.1 KB / 211 allocs`（内存 -44.5%）。
  - 架构整理（P1–P3）：
    - P1（`app/app.go`）：将 `App` 平铺状态归入 4 个值嵌套结构体（`side`、`pal`、`search`、`menu`）。
    - P2：同包拆分 3 个超千行文件——`client/client.go`（589 行）+ `client/apply.go`（383 行）+ `client/commands.go`（401 行）+ `client/workspace.go`（278 行）；`app/transcript.go`（873 行）+ `app/transcript_items.go`（225 行）+ `app/approval.go`（404 行）；`app/review.go`（847 行）+ `app/review_diff.go`（308 行）。
    - P3（`app/arch_boundary_test.go`）：新增 `TestArchBoundary` 守卫 `client` 不依赖 `app`/`kernel`/`mygo`，`app` 除 `kernel/rpc` 外不依赖 `kernel`。
  - 构建加速与产物瘦身：
    - `scripts/demo.sh` 默认快速直跑 `CGO_ENABLED=0 go run ./cmd/stratum`（跳过 `.app` 打包签名并复用 `${TMPDIR:-/tmp}/stratum-demo`），提供 `--dev`（`mygo dev` 热重载）、`--adhoc`（`mygo dev -sign -`）、`--fast` 与 `--fresh`。
    - `app/web/src/main.ts` + `app/web/build.mjs`：`File`/`FileDiff` 切换至 `@pierre/highlights` 内置 WASM 后端并裁剪未用的 Shiki 全量语法/主题，`bundle.js` 由 `11M`（`11,532,909` B）降至 `1.1M`（`1,134,638` B，`-90.16%`）；`CGO_ENABLED=0 go build -o /dev/null ./cmd/stratum` 实测 `0.889s`。
  - 模型、推理强度与用量悬停二级菜单重构（`app/menus.go`、`app/app.go`、`app/icons.go`、`app/icons/speedometer.svg`、`scripts/locales.py`、`app/menus_test.go`）：`modelPill` 改用紧贴主卡的 `PopoverBase`（`296pt`，含顶部「模型」分组与底部「查看用量」入口，点击主卡及子卡外空白处即刻关闭）；`Thinking effort`（`180pt`）与 `Usage`（`236pt`，展示账号额度与当前会话上下文）仅在悬停对应行时通过 `ui.Overlay` + `14px` 零缝隙桥梁弹出；测试 `TestTask8*`、`TestModelPickerHoverCascadeEffort` 全部通过。
- `0fc8e88`（包含上述 5 阶段 UI/UX 重构、1～4 项性能优化、P1–P3 架构整理、构建压缩与默认快启、悬停模型/强度/用量菜单）。之前 `ac89ae7`、`f605a22`、`7508174`、`22e41bf`、`17c1047`、`36c1180`、`e150bda`、`43dc519`、`b33adf9`、`c02a656`。工作区现有未提交修复（2 项）：
  - 侧栏项目行 `...` 下拉菜单修复（`app/sidebar.go`、`app/app.go`、`app/menus_test.go`）：`menuState.projectMenu` 记录当前打开菜单的 `cwd`，`row.Hovered() || menuOpen` 保持按钮挂载并用 `a.menuOptions(&menuOpen)` 同步开关，防止指针移向菜单项时因 `row.Hovered()` 变 `false` 导致菜单消失。
  - 浏览器默认下载 `Cannot create file` 修复（`app/webdata.go`、`app/webdata_test.go`）：默认 `BrowserDownloads == ""` 时改先暂存至 `TMPDIR`（避开 macOS `com.apple.WebKit.Networking` XPC 对 `~/Downloads` 的 TCC 限制），完成后由 Go 主进程移入 `~/Downloads`；若仍遇 `Cannot create file` 则触发 Go `http.Client` 回退下载。
- 电脑操控（computer use）**仍然隐藏**：只有设了环境变量 `STRATUM_COMPUTER_USE` 才出现。用户 2026-11-23 决定先保持隐藏，等真机验证后再说。不要自己去掉这个开关。
- 广告拦截：用户说不做。

## 待办（按优先级）

1. **真机复查**（要用户配合）：
   - `unsplash.com` 图片下载（`1d60183`）。
   - 电脑操控（`STRATUM_COMPUTER_USE=1`，用户自己的真实账号，当前先搁置）：授权卡片、点按、输入中文、剪贴板恢复、顶部提示是否不进截图、`ShowInactive` 是否不抢焦点、esc 停止、代理自己按 esc 时不停。
2. **人体工学余项**（对照 Apple 的做法）：**已完成（已推送 `c02a656`）**，见历史记录。
   - 仅保留真机视觉/触感确认：滚动边缘在真机上是否柔和自然、触控板触感反馈力度是否合适（WebView / 终端栏因不可遮挡内容保持非重叠布局）。
3. **终端余项**：**已完成（已推送 `c02a656`）**，见历史记录。
4. **浏览器余项**：**已完成（已推送 `c02a656`）**，见历史记录；「按时间范围清除浏览数据」用户确认不做。
5. **电脑操控余项**：**已完成（已推送 `c02a656`）**，见历史记录（截图遮挡未授权层-0 应用窗口；其余真机行为见第 1 项）。
6. **代码审查**：**已完成（已推送 `c02a656`）**，见历史记录（已审查 `4c87fac` 与 `730adfd`，并修复同步 `LoadURL` 失败时卡等 20s 的 bug）。

---

# 历史记录（旧的在前）

## 已完成（已推送到 `main`，截至 `207bf4d`）

1. **`55ee9f5`**：
   - 阶段 1 性能余项全部完成（通用 `lruCache`、`diffHash` 复用、`PreDecodeFrame`、`LoadOlder` 增量追加、`⌘F` 小写索引缓存、后台 FTS 回填、`OpenTurnSessions` 批量恢复）。
   - 阶段 4.3 全部完成（`syncAnnounce` VoiceOver 审批/轮次播报、`PaletteLerp` 主题切换渐变）。
   - 阶段 5.2–5.3 全部完成（`mygo.WebView` 接入 `@pierre/diffs` + `@pierre/trees` + `marked`，`app/web/` esbuild 单包 `go:embed` 嵌入）。
2. **`a0fa7a2`**：
   - 修复 Pierre WebView 加载 URL（`"/index.html"` → `"/"`）与空切片兜底。
   - 对齐 Files 工具栏 SVG 图标（16×16）与面板 `maximize-2` / `minimize-2` 图标。
   - 修复 `llm.Play` 演示模式摘要请求返回空文本报错。
3. **`207bf4d`**：
   - 右侧面板顶栏新增 `rv-close` 关闭按钮（14.5pt `x`），窄窗口浮层与最大化状态下均可收起。
   - 统一主区域与右侧顶栏控件高度为 `28pt`（`side-pill` 保持 `32pt` 浮层胶囊）。

## 已完成（已推送 `d36629f`）

1. **新建会话空页：方案 A（Craft 工作区概览）+ 方案 B（极简留白、输入框居中）合用**：
   - 内核 [workspace.go](kernel/rpc/workspace.go) 的 `workspace` 新增 `Commits`（HEAD 最近 2 条）和 `Recent`（磁盘上最近改动的 2 个文件，git 外也读，跳过 `Sensitive`）；`proto.Workspace` 加 `CommitSummary`、`RecentFile`。
   - [hello.go](app/hello.go)：文件夹有内容时画布中间是概览（头部「文件夹 · 分支」+「N 处未提交 / 没有未提交的改动」，未提交文件最多 3 个，最近提交，最近文件去重）；点未提交文件打开 Diff，点最近文件在 Files 打开；提交行不可点（没有单条提交的 Diff 视图）。
   - 文件夹什么都没有时没有概览，输入框画在画布约 40% 高度处，顶边固定、多行向下长；发出第一条消息后用 `springDefault`（ζ≈1.0，约 0.36s）从当前位置落到底部；回退到空会话时沿原路弹回；切换会话直接到位不动画；新会话等文件夹读完（最多 250ms）再淡入 160ms，按文件夹缓存上次结果。
   - 删掉「在 {folder} 里做点什么」；没加 `/` `@` 提示行（输入框占位文字已经写了）。
   - 顺手修：`kernel/workspace` 的 `Sensitive` 补上 `.stratum`（改名后漏了）。
2. **最近验证**：`go test ./...` 20 个包全部通过；`go build ./cmd/stratum ./cmd/stratumd` 通过；`TALOS_SNAPSHOTS` 导出空页截图人工核对过。

## 已完成（已推送 `2b17f57`）

1. **输入框改版：方案 A（Mail 式附带栏）**。托盘版照搬了 Antigravity，用户否决，已撤掉，单层卡片恢复原样。
   - 卡片顶部加「附带」栏（`attachedRow`，照 Mail 的收件人栏，分隔线隔开），没有内容时用 `springDefault` 收起。标记 `token` 照 NSTokenField：填色圆角、无描边；话里写的 `@文件`、`/技能` 用强调色，会话和附件用灰色；附件图片缩成 16pt 小图。原来的 `attachments`、`mentionChips` 改为 `attachmentTokens`、`mentionTokens`。
   - 发送按钮空闲时墨色 `idleInk = .38`。
   - 模型、权限胶囊加回 `chevron.up.chevron.down`（NSPopUpButton 的符号）；权限胶囊**不加任何图标**（用户明确不要）。
   - 居中状态固定输入文字顶边（`hero.words`），附带栏出现时向上长。
   - 排队消息文字改为左对齐。
2. **发送后输入框上跳再弹回**：原因是运行中卡片下多出一行 `liveStatus`（「1 秒 · esc 中断」，26pt），把卡片顶上去，结束时又收回。已删掉这行和 `runningFor`、`escToInterrupt`；新增 `TestComposerKeepsStillWhileRunning`（空闲→运行→结束，卡片 Y 不变）。卡片下的错误行 `a.err` 出现时仍会顶起卡片，没改。
3. **命令弹窗补设置**（[palette.go](app/palette.go) `settingOpts`）：语言、文字大小、减弱动态效果、按回车、三种通知与程序坞数字、什么时候通知；每个值一行、当前值不列，文案统一「设置：值」（新键 `settingTo`、`settingOn/Off`、`notifyKind`）。新增图标 `bell`、`a-large-small`、`accessibility`（Lucide，SF 对应 `bell`、`textformat.size`、`accessibility`）。测试 `TestPaletteSettings`。
4. **侧栏玻璃调弱**：主窗口、设置、定时任务三处侧栏共用 `sideTint`（[theme.go](app/theme.go)），透明度浅色 .38→.50、深色 .48→.58。测试环境没有窗口玻璃，只能真机看。
5. **附带栏只放附件和引用的会话**：`@文件`、`/技能` 已写在输入文字里，不再在附带栏重复（用户要求）。`mentionTokens` 换成 `sessionTokens`；删掉不再用的 `extractMentionChips`、`mentionChip`、`knownSlashToken`、`mentionTokenRe`；`token` 去掉强调色参数。会话引用不在文字里，所以留在附带栏。
6. **对话滚到底部抖动**：录屏逐帧量过，到底时内容下跳约 55pt 一帧再回，离开底部时一帧跳约 90pt。原因：输入框外层 `dock` 行没有 key，「回到底部」胶囊在它前面出现/消失时，`dock` 按位置认成胶囊那一格，`dock.Bounds().H` 在 87.3、0、32（胶囊高）之间跳，列表底部留白 `a.dockH+28` 随之跳，滚动范围和列表末端对不上。修法：`ui.Row(c.Key("dock"))`（[composer.go](app/composer.go)）。测试 `TestTranscriptEndScrollsSmoothly`（去掉 key 时失败）。
7. **输入框高亮**：`/技能`（只认已有技能/命令/提示）和 `@文件/文件夹` 用 `TextRanges` 着色（符号淡蓝、名字蓝色 500）；正在 @ 列表里写的词不着色；发出的消息也给 `@` 着色。`draftMarks`、`refTokens` 在 [mention.go](app/mention.go)。测试 `TestComposerMarksReferences`。
8. **侧栏底部 A1**（用户选定）：一行 32pt，账号名 + 额度环 + 百分比 + 上下箭头；去掉头像、第二行额度、齿轮。菜单：账号（勾选当前，副标题额度与重置时间）· 添加账号… · 设置… ⌘,；去掉「技能与 MCP」「账号」。额度环与上下文环共用 `ring`（[turns.go](app/turns.go)）。测试 `TestSidebarFooter`；`TestSettings` 改用 ⌘, 打开设置。
9. **Logo**：`design/icon/Stratum.icns`（程序坞）与 `app/icons/stratum.svg`（只在登录页）仍在。`heroMark` 与 `talos-hero-*.svg` 已无人调用，可删。
10. **验证**：`go test ./...` 20 个包通过；新增 `TestComposerAttached`；浅色、深色截图核对过。

## 已完成（已推送 `f5dc575`）

1. **左下角品牌栏与方案 B 浮层**（[sidebar.go](file:///Users/shqingda/Projects/stratum/app/sidebar.go)、[settings.go](file:///Users/shqingda/Projects/stratum/app/settings.go)）：
   - 触发行改为镂空单色 `brandMark`（`16pt`）+ `Stratum`（`13.5pt Semibold`）+ 额度环与百分比 + `chevron-up/down`（去掉登录态与多账号）。
   - 改用 `ui.Popover`（`248pt` 宽、左缘对齐向上弹出）：顶部 `p.Chip` 浅底圆角卡（`Stratum` + `还剩 N%` + 4pt 进度条 + `formatQuotaReset` 重置倒计时），下方仅留「设置… ⌘,」。测试 `TestSidebarFooter`（[sessions_test.go](file:///Users/shqingda/Projects/stratum/app/sessions_test.go)）已更新并截图核对。
2. **Stratum Logo 与 macOS 应用图标**（[stratum.svg](file:///Users/shqingda/Projects/stratum/app/icons/stratum.svg)、[stratum.py](file:///Users/shqingda/Projects/stratum/design/icon/src/stratum.py)、[build.sh](file:///Users/shqingda/Projects/stratum/design/icon/build.sh)）：
   - 瓷白渐变底 + 放大版黑曜石三横条为默认图标（[Stratum.icns](file:///Users/shqingda/Projects/stratum/design/icon/Stratum.icns)、[icon.png](file:///Users/shqingda/Projects/stratum/resources/icon.png)），并保留深色黑底款（[Stratum-dark.icns](file:///Users/shqingda/Projects/stratum/design/icon/Stratum-dark.icns)）。
3. **清理残留与依赖**：
   - 删除 `app/icons/talos-hero-*.svg`、`heroMark`、`design/icon/Talos.icns`、`design/icon/talos-*.svg`、`compare.png`、`preview.html` 及旧脚本 `glass/mech/orbit/real/small.py`；删除本地 `screenshot.png` 与 `design/fidelity/out/` 并写入 `.gitignore`。
   - 升级 `modernc.org/sqlite` `v1.62.0 → v1.63.0`；`app/web` 大版本依赖（`marked` 15、`typescript` 5.9、`esbuild` 0.25）保持锁定不升。
4. **验证**：`(cd app/web && npm run typecheck)`、`go test ./...`、`go build ./cmd/stratum ./cmd/stratumd` 全部通过。

## 已完成（已推送 `95e2c9b`）：改名遗留清理

1. **配置目录**：新增 `skills.DefaultRoot()`、`skills.ProjectDir(cwd)`（[skills.go](kernel/skills/skills.go)）：用 `.stratum`；只有旧 `.talos` 而没有 `.stratum` 时用 `.talos`（与 `auth.Dir` 同一规则）。改用它们的地方：hooks 读写、plugins 目录与安装、项目 skills 安装、`formatters.json`、memory 工具（项目与全局）。原来这些地方把全局目录写死为 `~/.talos`，与 `auth.Dir`（`~/.stratum`）不一致。
2. **读取两处的地方保持读两处**：项目 skills、`MEMORY.md`、commands、agents 同时读 `.stratum` 和 `.talos`。插件清单新增 `.stratum-plugin/plugin.json`，保留 `.talos-plugin`。
3. **沙箱**：[sandbox.go](kernel/sandbox/sandbox.go) 禁读清单补 `~/.stratum`（原来只禁 `~/.talos`，改名后数据目录可被沙箱内命令读取）。
4. **环境变量**：`TALOS_DEMO` → `STRATUM_DEMO`（`scripts/demo.sh`、`cmd/stratum`、`kernel/rpc/workspace.go`、`kernel/tools/web.go`、`kernel/preview/preview.go`，不再读 `TALOS_DEMO`）；`TALOS_MEMORY_DIR` → `STRATUM_MEMORY_DIR`；`TALOS_KIMI_OAUTH_HOST` → `STRATUM_KIMI_OAUTH_HOST`；hooks 同时注入 `STRATUM_SESSION_ID` 和旧的 `TALOS_SESSION_ID`；测试变量 `TALOS_SNAPSHOTS` → `STRATUM_SNAPSHOTS`，以及 `STRATUM_FAKE_LSP`、`STRATUM_TEST_MCP`、`STRATUM_ESCAPE_*`。
5. **数据库**：新增 `boot.DBPath()`：`auth.Dir/stratum.db`，只有旧 `talos.db` 时用旧的；`cmd/stratum` 和 `stratumd rpc -db` 默认值共用（`stratumd` 原来默认 `talos.db`）。
6. **文字**：`stratumd` 用法与提示、系统提示词「You are Stratum」、`Stratum does not allow this call.`、OAuth 回调页、离线演示回复、预览标题、Web 工具 `User-Agent`、MCP 客户端名 `stratum`、worktree 分支前缀 `stratum/` 与目录 `stratum-worktrees`、临时文件前缀、Go 注释、`mcpNote` 文案（`~/.stratum/mcp.json`，已跑 `go tool mygo generate`）、`scripts/locales.py`、`scripts/release.sh` 注释、`app/web/package-lock.json` 包名、`llm/testdata/README.md` 命令名。
7. **测试**：测试数据改名；新增 `TestStratumDirFallsBackToTalos`；`TestWorkspaceTalosSymlinkIsIgnored` 改为 `TestWorkspaceDirSymlinkIsIgnored`，覆盖 `.stratum` 和 `.talos`；`TestDefaultGlobalAndNoExecution` 保留 `~/.talos`，覆盖旧目录回退；hooks 测试检查两个会话 id 变量。
8. **有意不改（跳过）**：钥匙串服务名 `talos`（改了读不到已存凭据，且不能用真账号验证）；发给服务商的标识 `originator`/`agent_name_hint`/`referrer`/Copilot `Editor-Version`（改了以后登录是否可用不能验证；`llm/testdata` 录制文件也依赖它们）；`llm/testdata` 录制内容；`design/` 历史设计文档（`gaps.md` 按计划不在范围）；`kernel/workspace` 敏感路径正则已含两者。
9. **修复（HEAD 已有的问题）**：项目 `.stratum` 或 `.talos` 是指向工作区外的符号链接时，`Loader.Load` 会读入链接目标里的 `MEMORY.md`（`readRegular` 的 `O_NOFOLLOW` 只管最后一级）。改为 `readIn`：用 `os.OpenRoot` + `openChild` 读，不跟随链接（[skills.go](kernel/skills/skills.go)）。全局 `MEMORY.md` 不变。测试：`TestWorkspaceDirSymlinkIsIgnored` 加链接目标 `MEMORY.md`（修前失败）；新增 `TestMemoryFilesAreRead`（全局、`.stratum`、`.talos` 三处都读）。
10. **验证**：`go test ./...` 20 个包通过；`go build ./cmd/stratum ./cmd/stratumd` 通过；`go vet ./...` 无输出；没有新增未 gofmt 文件（14 个均为 HEAD 已有）。没有跑 `./scripts/demo.sh`。

## 已完成（已推送 `3d2f06c`）：OpenCode 代理与已有问题

1. **已有问题**：`app/settings.go` 的 `rows` 不再存 `*ui.Context`（方法改为传 `c`），`go tool mygo vet .` 通过；14 个文件已 gofmt；`scripts/locales.py` 已与 JSON 同步（补 10 个键、删 2 个旧键，重新生成的 JSON 与提交版一致）。
2. **内核代理**：`kernel/agents` 加 `TopP`、`Bash` 规则（按命令前缀、`*` 通配、最长规则优先；链式命令逐段判，含 `$(`/反引号/重定向时 allow 不生效）、`Agent.Decide`（deny 拒绝；ask 在模式允许时也问；allow 只放行工作区内、非敏感、沙箱内的调用）、`Primary()`/`Sub()`/`Named`；解析 front matter 二级 `permission.bash`、`opencode.json(c)` 的 `agent` 键、注释与尾逗号；`Load`/`Find` 加插件目录参数。
3. **会话**：`store`/`proto` 加 `agent` 列与字段（迁移在 `migrateFlags`）；`Update` 校验主代理；`runAgent` 用主代理过滤工具、追加提示词、模型（`provider/model` 或本账号模型，`agentTarget`）、`temperature`/`top_p`/`max_steps`；子代理同样接入模型与采样；`runner.policy` 在模式判定后再按代理权限判；`@子代理名` 在消息后附路由说明（同名文件优先）；插件包 `commands`、`agents` 目录（manifest 字段可为字符串或数组）并入命令与代理列表；`MCPList.Agents` 给界面。
4. **llm**：`Request.Temperature/TopP`；Anthropic（思考时不发）、Chat Completions、Gemini 发送；Responses（ChatGPT 后端）不发。
5. **界面**：输入框工具栏加主代理胶囊（`app/agents.go`，菜单照权限胶囊）；输入框里 `Tab` 切换主代理（`⇧Tab` 仍切权限模式）；`@` 列表首组「代理」列子代理。代理列表在打开菜单或按 Tab 时才读（每帧读会多发 `mcp.list`）。
6. **测试**：`kernel/agents`、`kernel/session/agents_test.go`、`llm/sampling_test.go`、`app/agents_test.go`；当时 `go test ./kernel/... ./llm/... ./app` 通过。
7. **设置页**：「技能与 MCP」分段改为 MCP 服务器 · 代理 · 钩子 · 插件 · 格式化。「代理」分主代理与子代理两组，每行写说明与来源（内置或文件路径）；「格式化」列 `.stratum/formatters.json` 规则与内置 gofmt（内核 `MCPList.Formatters`）。`extensions_test.go` 的分段序号随之改为 2、3。
8. **跳过**：Themes（用户定的色板是 Mono，多主题与之冲突；外观页已有自动/浅色/深色）；Policies 独立页（权限页已有「总是允许」规则列表；代理自己的权限写在代理文件里）。
9. **验证**：`go test ./...` 20 个包通过；`go build ./cmd/stratum ./cmd/stratumd` 通过；`go vet ./...`、`go tool mygo vet .` 通过；`gofmt -l` 为空；`npm run typecheck` 通过。没有跑 `./scripts/demo.sh`。

## 已完成（已推送 `1e82861`）：终端与人体工学细节（2026-11-23）

1. **「终端」页签改为真 shell**（[terminal.go](app/terminal.go)）：用 `mygo/plugins/terminal`（libghostty-vt）。原来的代理命令日志视图删除（对话里的命令卡片已有同样内容）。
   - 在会话文件夹里打开用户的登录 shell，开 zsh 提示符集成；主题取面板底色 `p.Sheet` 和 `p.Fg`，ANSI 色板用插件自带的 VS Code 色板。
   - 每个会话可以有多个标签（照 Claude 桌面版的「+」和 Terminal.app 的标签）：`+` 或 ⌘T 新建；悬停显示左侧关闭按钮；shell 正常退出（码 0）时关标签，非 0 时留着（Terminal.app 默认）。
   - ⌃`（Codex、Claude 桌面版）开合终端；显示 › 打开终端面板 菜单项同键；⌘K 命令面板里的「打开终端面板」也会启动 shell。
   - 只在用户打开终端时启动 shell；终端页签显示时切换会话不会自动启动（显示「没有打开的终端」和「新建终端」）。
   - 终端获得焦点时，窗口不注册 esc、⏎、⇧⇥、⌥↑、⌥↓（这些键交给 shell）；⌘Z 不撤销归档。
   - ⌘F 在终端获得焦点时查终端（右上角浮动查找栏，⌘G / ⇧⌘G、⏎、↑↓，esc 关闭）；否则查对话。菜单「在会话中查找」同样按焦点分流。
   - ⌘W 在终端获得焦点时关当前标签（Terminal.app）。前台有任务时先弹确认框「要终止这个终端里的进程吗？」并写出进程名。判断方法：`ps` 找本进程的、自带终端的会话首进程（按 pid 顺序对应标签创建顺序），比较 `tpgid` 与 `pgid`。`ps -E` 在本机不显示环境变量，所以没用环境变量做标记。
   - 会话被删除后，它的 shell 被挂断（`sweepTerminals`）。
   - 原生库：`go test`/`go run` 第一次运行时下载到 `~/Library/Caches/mygo/natives/`（本机已下载，用户同意）；`mygo build`/`mygo dev` 会把库打进 App。测试默认只在缓存里已有库时跑真 shell，`STRATUM_TERMINAL_TESTS=1` 允许下载。
2. **人体工学**：
   - 输入框错误行移到卡片上方，卡片不再被顶起（`TestComposerKeepsStillWithAnError`，用旧代码跑会失败：y 736.5 → 706.5）。
   - 侧栏和右侧面板宽度跨启动保存（`proto.Settings.SidebarWidth`、`InspectorWidth`，拖完松手时存；双击恢复默认时存 0）。
   - 分隔线与指针一比一跟随：拖过上限后分隔线停住，指针回来到原位才再动（NSSplitView）。
   - 侧栏拖过最小宽度一半时收起（Finder、Mail），⌘\ 恢复原宽度；右侧面板拖过最小宽度一半时关闭（Xcode 检查器）。
   - 定时任务行的 `OK`/`Error` 改为本地化的「成功」「失败」。
3. **文案**：新增 `newTerminal`、`closeTerminal`、`noTerminal`、`terminalFailed`、`terminateTitle`、`terminateBody`、`terminateBodyAny`、`terminate`、`findInTerminal`、`succeeded`；删除 `noCommandsYet`。已跑 `python3 scripts/locales.py && go tool mygo generate`。
4. **测试**：`app/terminal_test.go`（启动失败提示、只在打开时启动、真 shell 在会话文件夹、esc/⇧⇥/⌥↓ 进 shell、标签与 exit、⌘W 与终止确认）；`TestTerminalAndPlan` 改为 `TestPlanPane`；`TestErgonomicsSplitWidths` 改写。
5. **验证**：`go test ./...` 全部通过；终端测试 `-count=5` 通过；`go vet ./...`、`go tool mygo vet .`、`gofmt -l` 为空、`go build ./cmd/stratum ./cmd/stratumd`、`npm run typecheck` 通过。浅色、深色终端截图核对过（两个标签、ANSI 颜色、查找高亮）。**没有跑 `./scripts/demo.sh` 真机**。

## 已完成（已推送 `f23fc80`）：终端配色与光标

1. **终端配色改为 Kero 的 paper**（[terminal.go](app/terminal.go) `termTheme`/`keroTheme`）：从 `github.com/egoist/kero` 的 `theme.go` 移植 `toneTheme`（MIT，注释写明出处），用 paper 的 `tone{260, 0, 0.9, 262}`。用户的 `~/.config/kero/config` 是 `theme = paper`。背景用面板的 `p.Sheet`（不出现方框），16 色、前景、选区、蓝色光标都取自 paper。Kero 里 Rex 的配色叫 petal（`tone{350, 1, 0.95, 350}`），本轮没用。
2. **光标改为闪烁竖线**（`Cursor: terminal.CursorBar`；插件默认闪烁），与用户 Kero 的 `cursor-style = bar` 一致。
3. **验证**：`go test ./...` 中 `llm/auth` 的 `TestQuotaHeadersAndFastCache` 一次失败（TempDir 清理时目录非空），单独 `-count=5` 重跑通过，本轮没改 `llm/`；其余全部通过。`go vet ./...`、`go tool mygo vet .`、`gofmt -l` 通过。浅色、深色快照用临时测试打印 30–37、90–97、粗体、暗淡后核对过；临时测试已删。**没有跑 `./scripts/demo.sh` 真机**。
4. 用户问过的事实：shell 是用户的 `$SHELL`（`/bin/zsh`，插件 `terminal.go:799`），截图里是 pure 提示符；默认文件夹是会话的 `Cwd`，演示模式下是 `scripts/demo.sh:24` 的 `mktemp -d` 目录。

## 已完成（已推送 `4c87fac`）：面板内可交互浏览器

1. **「预览」页签改名「浏览器」**（[browser.go](app/browser.go)）：WebView（`Untrusted`，不能调用 Go），Safari 式工具栏。
   - 后退、前进：第一页时变暗。
   - 地址栏：按 Safari 规则读主机名或搜索（本机地址用 http，其他用 https，含空格或无点号时用 Google 搜索）。
   - 重新载入和停止互换；「在浏览器中打开」。
   - 新窗口链接在当前页打开；`mailto:` 等非网页协议交给系统；支持触控板滑动前进后退和捏合缩放。
   - 没有 WebView（测试、`stratumd`）时退回旧的无头预览。
2. **代理工具 `preview` 改为 `browser`**（[kernel/tools/preview.go](kernel/tools/preview.go)、[kernel/preview/browser.go](kernel/preview/browser.go)）：通过 `preview.Driver` 驱动同一个页面，用户看得到代理做什么。
   - 动作：open、read、click、type（含 select、contenteditable、submit）、scroll、back、forward、reload、screenshot、console、eval。
   - 结果带控制台错误、失败请求和带编号 ref 的元素列表。
   - 权限：本机页面直接运行；网上的网站按 `Net` 询问（与打开网站相同）。
3. **验证**：真实 WKWebView 检查程序 12 项全部通过，程序存在 scratchpad 的 `webcheck.go`，没有提交。假页面单元测试和工具栏快照也通过。**没有跑 `./scripts/demo.sh` 真机**。

## 已完成（已推送 `45bee24`）：电脑操控（computer use）

1. **设置 › 通用 › 电脑操控**：默认关闭，打开后代理才看得到 `computer` 工具。只给支持图像的模型。
2. **`computer` 工具**（[kernel/tools/computer.go](kernel/tools/computer.go)、[kernel/computer](kernel/computer)）：
   - 动作：screenshot、click、double_click、right_click、move、drag、type、key、scroll、open_app、wait。
   - 截图缩放到 1280×800 以内；坐标按截图像素换算为点。
   - 每次动作后返回新截图，作为图像交给模型（新增 `Tool.RunImages`）。`browser` 的 screenshot 也交图像。
3. **macOS 实现**：不用 cgo。
   - 鼠标和按键用 JXA 调 CoreGraphics。
   - type 用剪贴板粘贴，之后恢复剪贴板，以免中文输入法截获按键。
   - 截图用 `screencapture` 加 `sips`。
   - 缺权限时用 `permissionflow.Guide` 引导到系统设置（辅助功能、屏幕录制）。
4. **权限**：新种类 `perm.Computer`，除完全访问外每次都问。审批卡片显示「电脑操控」和理由。顺带修复：`Net` 原来显示「涉及可能存有密钥的文件」，现在显示「要访问网上的网站」。
5. **验证**：
   - 已验证：假桌面单元测试（坐标换算、按键解析、权限引导）、全部 JXA 脚本用 `osacompile` 编译通过、只读的屏幕尺寸脚本可以运行。
   - 未验证：没有在真机上发送过鼠标或键盘事件（不能操控用户桌面）；剪贴板恢复没有实际运行过。
   - `go test ./...`：只有 `llm/auth` 的 TempDir 清理竞争偶发失败（3 次中 1 次），本轮没有改 `llm/`。

## 已完成（已推送 `730adfd`）：浏览器对齐主流 agent

对照对象：Claude 桌面版浏览器面板（`mcp__Claude_Browser__*`）和 Playwright MCP。

1. **代理 `browser` 工具新动作**（[kernel/preview/browser.go](kernel/preview/browser.go)、[kernel/tools/preview.go](kernel/tools/preview.go)）：
   - `key`：在焦点元素上按键，带浏览器默认行为（Enter 提交或点按钮、Tab 移焦点、字符输入、Backspace、方向键和翻页键滚动）。
   - `hover`、`wait`（等文字出现，或等秒数，最多 30 秒）、`network`、`console`（全部级别）。
   - 标签页：`tabs`、`new_tab`、`select_tab`、`close_tab`。`read` 在多于一个标签页时说明数量。
   - 对话框：代理动作打开的 alert/confirm/prompt 由代理回答，默认取消；传 `accept_dialog`、`dialog_text` 时确认或填写。`read` 用「Dialog:」报告。
   - 被操作的元素闪一下蓝框（#0A84FF）。
   - 权限：`new_tab` 打开网站、在网站上 `key`/`hover` 按 `Net` 询问。
2. **面板浏览器多标签页**（[app/browser.go](app/browser.go)、[app/keys.go](app/keys.go)）：
   - 两个以上标签页时显示 Safari 式标签栏；关闭按钮在标签右侧（当前标签或悬停时显示）。
   - 关闭当前标签页后，右侧的标签页到前面；关闭最后一个时，左侧的到前面。
   - 「+」新建标签页；`target=_blank` 和带用户手势的 `window.open` 打开新标签页。
   - ⌘T 新建标签页（终端显示时仍归终端）；⌘R 重新载入。
   - 代理操作后 3 秒内，地址栏显示「代理正在使用」并加强调色描边。
3. **验证**：
   - 已验证：真实 WKWebView 检查程序 29 项全部通过（按键、悬停、四种对话框、等待、控制台、网络、标签页、代理点击 `target=_blank` 时 `UserInitiated` 为真）。程序在本会话 scratchpad：`/private/tmp/claude-501/-Users-shqingda-Projects-stratum/c6901146-0428-4cbb-b487-72e89eeb0cf5/scratchpad/webcheck.go`，没有提交。
   - 已验证：`go test ./...`、`go vet ./...`、`go tool mygo vet .`、`GOOS=linux go build ./kernel/... ./cmd/stratumd` 都通过。
   - 未验证：没有跑 `./scripts/demo.sh` 真机。

## 已完成（已推送 `f0aedc7`）：隐藏电脑操控

- 原因：没有达到主流 agent 的成品水平（没有按应用授权和应用分级，只能整屏操作）。
- 只有设置了环境变量 `STRATUM_COMPUTER_USE` 时，内核才提供 `computer` 工具，设置 › 通用才显示开关（`computer.Enabled()`、`computerUseShown()`）。代码保留。

## 已完成（已推送 `fdd84a5`…`2c1530f`）：浏览器、终端与对话抖动（2026-11-23）

1. **对话抖动**（`f3c8492`）：点进会话、收起和展开侧栏时，输入框底座被重建，第一帧高度为 0，列表末尾跳一下。修复：`app/composer.go` 沿用上次高度；`app/app.go` 主区域加 key `sheet`；「回到最新」按钮等列表显示过一帧再出现。测试 `app/jolt_test.go`（逐帧像素比较，只推进时钟、不补跑稳定帧）。
2. **浏览器**（参照：Cursor 浏览器 ⋯ 菜单有 Clear Cookies/Clear Cache；Codex/ChatGPT 设置 › Browser 有历史记录、Clear browsing data 按种类、下载位置和「Ask where to save downloads」；Safari 工具栏下载按钮）：
   - 工具栏：新建标签页右侧加「下载」和「⋯」（`app/webdata.go`）。⋯：清除 Cookie、清除缓存（立即执行并提示）、清除浏览数据…、下载、浏览器设置…。
   - 清除浏览数据对话框：勾选浏览历史记录、Cookie 和网站数据、缓存的图片和文件。入口：⋯、设置 › 浏览器、⌘K「清除浏览器历史记录和 Cookie…」、应用菜单「清除历史记录…」（全选）。`UseBrowser` 的 wipe 改为 `func(app.WebData) error`，`cmd/stratum/main.go` 映射到 `mygo.SiteData`。没有时间范围：mygo `Session.ClearData` 不接受时间。
   - 历史记录：页面加载完成时记录（只记 http/https），存 `用户数据目录/browser-history.json`，退出时写盘，最多 5000 条。设置 › 浏览器可搜索、点开、逐条移除。地址栏输入时显示最多 6 条建议，↓↑ 选、回车或点击打开、Esc 收起。
   - 下载：默认存到「下载」；设置 › 浏览器可改文件夹、恢复默认、打开「下载前询问存放位置」（先下到临时文件夹，完成后弹存储面板再移动；取消则删除）。下载列表显示大小和来源，点击在访达中显示，「清除」只清列表。
   - 标签页跨重启恢复、Web 检查器（`DevToolsEnabled`）：`c563efc`。
   - 测试：`app/webdata_test.go`、`app/browser_test.go`。全部为离屏测试；未跑 `./scripts/demo.sh`，真实下载、真实清除 Cookie 未验证。
3. **终端**（`c563efc`）：标签页的文件夹和末尾 512 KB 历史输出跨退出保留（`app/termkeep.go`，`terminals.json`），恢复时灰色显示并加一行「已恢复之前的内容」。
4. **广告拦截**：用户说不做。

## 已完成（已推送 `2f6a5c6`）：电脑操控对齐 Claude 的 computer use（2026-11-23）

参照：Claude 桌面版 computer use（`request_access` 按应用授权、三种分级、`switch_display`）。

1. **按应用授权**（`kernel/tools/computer.go`、`kernel/computer/access.go`）：
   - 新动作 `request_access`（`apps`、`reason`）。任何权限模式都要问用户，完全访问也一样；代理权限（agents）不能放行；旧的「始终允许 computer」规则不再生效（`kernel/perm/perm.go` `Decide`、`kernel/session/runner.go`）。
   - 审批卡片列出每个应用和分级（`app/transcript.go` `appsChip`），没有「始终允许」。`proto.Approval.Apps`。
   - 授权按会话保存在内存，Stratum 退出或删除会话时清除。授权前任何动作（含截图）都报错，提示先 `request_access`。
   - 已授权应用里的动作不再逐次询问（`perm.Request.Granted`）。
2. **分级**（`computer.TierOf`，按 bundle ID 和名称）：浏览器 read（只能截图里看到）；终端和代码编辑器 click（左键点按、滚动；不能输入、按键、右键、拖拽）；其余 full。Stratum 自己永远不能授权。
3. **执行时检查**：鼠标动作检查点下窗口所属应用（`CGWindowListCopyWindowInfo`）；桌面和程序坞算 Finder；菜单栏算前台应用且要 full；输入和按键检查前台应用且要 full；`open_app` 要已授权。
4. **多显示器**：`switch_display`（编号或名称）；截图用 `screencapture -R` 截所选显示器；坐标换算到全局坐标；结果文字列出所有显示器。
5. **「正在使用你的电脑」提示和 esc 停止**（`app/control.go`、`cmd/stratum/main.go`）：一轮对话第一次操作时，主显示器顶部居中显示浮动面板（Panel、鼠标穿透、内容保护、不抢键盘）；注册全局 Esc，按下停止该轮（同 ⌘.）；代理自己按 esc 时临时放开。该轮结束或会话删除时隐藏。
6. **验证**：
   - 已验证：`go test ./...`、`go vet ./...`、`gofmt`、`GOOS=linux go build ./kernel/... ./cmd/stratumd` 通过。假桌面单元测试覆盖授权、分级、菜单栏、Finder、Stratum 自身、多显示器、提示与 esc（`kernel/tools/computer_test.go`）；会话层测试覆盖完全访问模式和旧规则仍要询问（`kernel/session/computer_test.go`）；卡片、提示面板、Esc 绑定顺序（`app/computer_test.go`）。
   - 已验证（真机只读）：显示器列表、按名找应用、前台应用、点下应用的 JXA 脚本在本机运行正确（`TestReadsTheDesktop`）。JXA 的 `NSArray.count` 返回字符串，要写 `count > 0`。
   - 未验证：没有发送过真实鼠标键盘事件；提示面板是否真的不出现在 `screencapture` 截图里（`SetContentProtection`）、`ShowInactive` 是否不抢焦点、Panel 窗口层级是否非 0（点下应用检查靠它跳过面板）、全局 Esc 是否拦截代理自己发的 esc。
7. **仍隐藏**：没有去掉 `STRATUM_COMPUTER_USE` 开关。真机试用：`STRATUM_COMPUTER_USE=1` 启动，设置 › 通用打开「电脑操控」，用真实模型账号（用户自己）。

## 已完成（已推送 `c02a656`）：人体工学余项（2026-11-23）

1. **Core Text 字距验证**（`app/ergonomics_test.go` `TestCoreTextOpticalTracking`）：
   - 离屏实测 `Stratum 1234567890`：`11pt`（`LetterSpacing(0)`）宽 `112.084`，`11pt+0.11` 宽 `114.064`，`22pt` 宽 `208.218`，`2*w11 / w22 = 1.0766`（+7.66%）。
   - 结论：macOS Core Text 下系统字体已按字号自动应用光学字距，因此保留 26 处 10–11.5pt 小字不叠加手写正字距。
2. **原生滚动面板 `glass.ScrollEdge` 浮层改造**（参照 Apple macOS 滚动边缘 / 本项目 `app/settings.go`）：
   - `app/filetree.go`（`filePreviewView`）：34pt 顶栏改为绝对定位浮层 + `glass.ScrollEdge{Background: p.Sheet}`，移除 0.5pt 底边线，Markdown / 代码 / 加载与错误状态内容顶部避让 `34pt`。
   - `app/panes.go`（`filesPane`、`planPane`）：`filesPane` 搜索框改为绝对定位浮层 + `glass.ScrollEdge{Background: p.Sheet}`（树与搜索列表顶部避让 `48pt`），split 空状态顶栏移除 0.5pt 底边线；`planPane` 顶部增加 16pt `glass.ScrollEdge{Background: p.Sheet}`。
   - `app/review.go`（原生 Changes）：`a.pierre.wv == nil` 时，40pt scope 栏改为浮层 + `glass.ScrollEdge{Background: p.Sheet}`，移除 0.5pt 底边线。
   - Browser、Pierre WebView 与 Terminal 保持非重叠布局，避免浮层遮挡网页顶部或终端首行。
   - 新增离屏测试 `TestErgonomicsScrollEdgePanes`。
3. **macOS 触控板触感反馈**（参照 Apple `NSHapticFeedbackManager`）：
   - 新增 `app/haptic_darwin.go`（使用 `github.com/ebitengine/purego/objc` 调用 `[NSHapticFeedbackManager defaultPerformer]`，内置 `testing.Testing()` 保护，测试中不触发真机硬件；无 cgo 依赖，兼容 `CGO_ENABLED=0`）和 `app/haptic_other.go`。
   - 接入 `app/app.go`（分栏拖过收起阈值、双击重置宽度触发 `hapticAlignment`）、`app/panes.go`（切换右侧面板页签触发 `hapticLevelChange`）与 `app/review.go`（切换 Changes 作用域触发 `hapticLevelChange`）。
   - 新增测试 `TestErgonomicsHapticFeedback`。
4. **验证**：
   - 已验证：`gofmt -l app kernel cmd proto locales llm client` 无输出、`CGO_ENABLED=0 go build ./...`、`go vet ./...`、`go tool mygo vet .`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go test ./...` 全部通过。
   - 未验证：未跑 `./scripts/demo.sh` 真机确认 `glass.ScrollEdge` 视觉过渡与物理触控板触感力度。

## 已完成（已推送 `c02a656`）：终端余项（2026-11-23）

1. **在终端中打开**（参照 Apple Finder / Claude 桌面版）：
   - `scripts/locales.py` 新增 `openInTerminal`（「在终端中打开」/「Open in Terminal」），并重新生成 `locales/`。
   - `app/terminal.go` 新增 `openTerminalIn(dir string)`，打开右侧终端面板并在目标目录新建终端标签页。
   - `app/sidebar.go`（`projectGroup` 项目组菜单）、`app/filetree.go`（目录树文件夹右键菜单）与 `app/panes.go`（文件搜索结果文件夹右键菜单）接入「在终端中打开」。
2. **同名终端标签页区分与路径提示**：
   - `app/terminal.go` 新增 `termTabLabel`（同标题标签从第二个起追加 `(2)`、`(3)`）与 `termTabTooltip`（悬停提示完整目录路径），并补充 `termTitle` 空指针防护。
3. **验证**：
   - 新增 `TestTerminalDuplicateTabLabels`、`TestOpenInTerminalDir`（`app/terminal_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

## 已完成（已推送 `c02a656`）：浏览器余项（2026-11-23）

1. **焦点感知快捷键**（参照 Safari 与本应用 `termFocused` 模式）：
   - `app/browser.go` 新增 `browserFocused(c)`，绑定页面视图与地址栏焦点。
   - `app/keys.go`、`app/app.go`：焦点在浏览器内时，`⌘L` 聚焦地址栏、`⌘W` 关闭当前网页标签、`⌘[` / `⌘]` 网页后退/前进、`⌘T` 新建网页标签；焦点不在浏览器内时保留原有全局行为（聚焦输入框、关闭窗口/浮层、切换会话、终端优先）。
2. **代理浏览器 `upload` 与 `drag` 动作**：
   - `kernel/preview/browser.go`：`Action` 新增 `ToRef`、`Paths`、`Files`，`pageJS` 新增 `upload(ref, files)`（构造 `DataTransfer` + `File` 并触发 `input`/`change`）与 `drag(fromRef, toRef)`（派发指针/鼠标/拖拽事件）。
   - `kernel/tools/preview.go`：`Browser` 工具补充 `upload`（工作区文件读取、10 MiB 单文件 / 20 MiB 总量限制、MIME 检测、`pathAccess` 权限校验）与 `drag`，并纳入非本地页面 `perm.Net` 校验。
3. **验证与遗留**：
   - 新增 `TestBrowserFocusShortcuts`（`app/browser_test.go`）、`TestBrowserUploadAndDrag`（`kernel/preview/browser_test.go`）、`TestBrowserUploadAndDragTool`（`kernel/tools/preview_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。
   - 阻塞：清除浏览数据时间范围因 `mygo.Session.ClearData` 接口不接受时间参数而跳过。

## 已完成（已推送 `c02a656`）：电脑操控截图隐藏未授权应用 & 浏览器代码审查（2026-11-23）

1. **电脑操控截图遮挡未授权窗口**：
   - `kernel/computer/computer.go` 新增 `Window` 与可选 `WindowLister` 接口。
   - `kernel/computer/computer_darwin.go` 实现 `Windows(ctx)`，通过只读 `CGWindowListCopyWindowInfo(1 | 16, 0)` 从前到后返回 layer-0 可见窗口及所属应用。
   - `kernel/computer/redact.go` 新增 `RedactScreenshot`，按窗口前后遮挡顺序将未授权应用覆盖区域涂暗为 `#1C1C1E`（前面已授权的窗口不被后面未授权窗口遮挡），全部已授权时原样返回 JPEG。
   - `kernel/tools/computer.go` 截图后自动按 `computer.GrantFor(env.SessionID, app)` 过滤遮挡。
   - 新增 `TestRedactScreenshot`（`kernel/computer/computer_test.go`）。
2. **代码审查（`4c87fac`、`730adfd`）与修复**：
   - 审查结论：WKWebView 隔离与无 Go 绑定边界安全、`BrowserDriver` UI 线程投递与锁使用无死锁风险；发现 1 处缺陷——`app/browser.go` `load` 在 `b.page.LoadURL` 同步返回错误时未推进 `b.loads` / `b.err`，会导致 `BrowserDriver.OpenTab` / `Navigate` 的 `b.wait` 空等 20 秒超时。
   - 已修复 `app/browser.go` `load`，并在 `app/browser_test.go` 新增 `TestBrowserDriverLoadErrorDoesNotHang`。
3. **验证**：
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

## 已完成（已推送 `b33adf9`）：运行时阻止系统休眠（2026-11-23）

1. **设置与文案**（参照 Codex 桌面版「Prevent sleep while running」与 Zed「生成期间阻止系统休眠，等确认时不阻止」）：
   - `proto/proto.go` `Settings` 新增 `PreventSleep bool`。
   - `scripts/locales.py` 新增 `preventSleep`（「运行时阻止系统休眠」/「Prevent sleep while running」）并重新生成 `locales/`。
   - `app/settings.go`（通用设置 › 工作时）与 `app/palette.go`（命令面板）接入开关。
2. **macOS 防休眠与状态联动**：
   - 新增 `app/awake_darwin.go`（通过 `/usr/bin/caffeinate -i -w <pid>` 阻止系统空闲休眠，内置 `testing.Testing()` 保护）与 `app/awake_other.go`。
   - `app/desktop.go` 新增 `SleepBlocker` 接口，并在 `watch()` 中仅当 `PreventSleep` 开启且存在 `run` 状态会话时激活（`wait` 等待确认时不阻止）。
3. **验证**：
   - 新增 `TestDesktopPreventSleepWhileRunning`（`app/desktop_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

## 已完成（已推送 `43dc519`）：`/btw` 旁支对话（2026-11-23）

1. **协议与内核**（参照 Claude Code 桌面版 `⌘;` / `/btw`）：
   - `proto/proto.go` 新增 `MethodSideAsk`（`side.ask`）、`SideAsk`、`SideAskResult`。
   - `kernel/session/side.go` 新增 `SideAsk`，基于当前会话历史快照直接向模型提问：不带工具（`Tools: nil`）、不写库、不打断运行中的主轮次；`kernel/rpc/rpc.go` 接入分发。
2. **客户端与界面**：
   - `client/client.go` 新增临时状态（`SideSession`、`SideQuestion`、`SideAnswer`、`SideErr`、`SidePending`）、`SideAsk(text)` 与 `ClearSide()`，切换或新建不同会话时自动清理。
   - `scripts/locales.py` 新增 `sideAsk`、`sideAskNote`、`sideAskPlaceholder`、`sideAskBadge` 并重新生成 `locales/`。
   - `app/side.go`、`app/app.go`、`app/keys.go`、`app/slash.go`、`app/composer.go`、`app/palette.go`：支持 `⌘;` 打开/收起浮层、`/` 菜单 `/btw`、输入框直接发 `/btw <问题>`、`Esc` 与 `⌘W` 关闭及复制回答。
3. **验证**：
   - 新增 `TestSideAskReadOnlyAndEphemeral`（`kernel/session/side_test.go`）与 `TestSideAskShortcutAndSubmit`（`app/side_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

## 已完成（已推送 `e150bda`）：终端测试隔离 `HISTFILE`（2026-11-24）

- `app/terminal_test.go` `termApp` 启动 `/bin/sh` 时补充 `HISTFILE=/dev/null` 与 `HISTFILESIZE=0`，避免跑测试时向用户真实 `~/.bash_history` 写入历史。

## 已完成（已推送 `36c1180`）：标未读快捷键、编辑并重发、回退菜单数字直选（2026-11-24）

1. **`⌘⇧U` 标未读**（参照 Codex 桌面版）：
   - `app/keys.go` 新增 `⌘⇧U` 调用 `markUnread`，`app/palette.go` 为「标记为未读」补充快捷键提示 `⇧⌘U`。
2. **已发送消息「编辑并重发」**（参照 Zed / Claude 桌面版）：
   - `scripts/locales.py` 新增 `editAndResend`（「编辑并重发」/「Edit & Resend」）并重新生成 `locales/`。
   - `app/rewind.go` 新增 `editAndResend` 与 `editResendButton`，回退对话到该轮并把原提问填回输入框聚焦；`app/transcript.go` 接入用户消息右键菜单与悬停操作栏。
3. **回退菜单 `1–9` 数字键直选**（参照 Claude Code）：
   - `app/rewind.go` `rewindKey` 支持按 `1`–`9` 直接选中并确认对应条目，`rewindMenu` 与 `rewindActionRows` 前 9 项右侧显示数字序号提示。
4. **验证**：
   - 新增 `TestErgonomicsUnreadShortcutEditResendAndRewindDigits`（`app/ergonomics_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

## 已完成（已推送 `17c1047`）：模型选择器搜索、收藏与最近使用（2026-11-24）

1. **设置与文案**（参照 Zed / OpenCode / Codex）：
   - `proto/proto.go` `Settings` 新增 `FavoriteModels []string` 与 `RecentModels []string`（按 `account/id` 存储）。
   - `scripts/locales.py` 新增 `searchModels`、`favoriteModels`、`recentModels`、`allModels`、`favoriteModel`、`unfavoriteModel` 并重新生成 `locales/`。
2. **界面与交互**（`app/menus.go`、`app/app.go`）：
   - `modelPill` 改为 `ui.Popover`，顶部提供实时搜索框（`Esc` 关闭、`Enter` 选首项），列表按「收藏」「最近使用」「全部模型」分组，每行带置顶/取消收藏按钮，底部保留推理强度选择与不支持提示。
3. **验证**：
   - 新增 `TestModelPickerSearchFavoriteAndRecent`（`app/menus_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

## 已完成（未提交）：自动/重新生成标题、回复后建议、按住 `⌘` 显示侧栏编号（2026-11-24）

1. **自动生成与重新生成会话标题**（参照 Zed / T3 Code / Claude 桌面版）：
   - `proto/proto.go` 新增 `MethodSessionRetitle`（`session.retitle`），`kernel/rpc/rpc.go` 接入分发。
   - `kernel/session/route.go` 新增 `Retitle(ctx, sessionID)`，基于会话首尾消息摘要生成新标题并落盘广播；首轮自动命名 `retitle` 在未配置 `Settings.Light` 时自动回退到当前会话模型（排除测试 `*llm.Script`）。
   - `client/client.go` 新增 `Retitle(id)`；`app/sessions.go`（会话右键菜单）、`app/palette.go`（命令面板）与 `app/slash.go`（`/retitle`）接入入口。
2. **回复后灰色建议与 `Tab` 接受**（参照 Claude Code）：
   - `proto/proto.go` 新增 `EventSuggestion`（`turn.suggestion`）、`Suggestion` 与 `Settings.NoSuggestions`。
   - `kernel/session/route.go` 与 `session.go` 在正常轮次结束且无排队提示时异步生成下一句建议；`llm/play.go` 补充演示模式标题、建议与 `/btw` 响应。
   - `app/composer.go` 在空输入框占位符展示建议及 `⇥ Tab` 可点击胶囊，`app/keys.go` 支持 `Tab` 接受、`Esc` 清除；`app/settings.go` 与 `app/palette.go` 提供开关。
3. **按住 `⌘` 在侧栏显示 `⌘1–9` 键帽**（参照 OpenCode）：
   - `app/sidebar.go` `sessionRow` 在仅按住 `⌘` 时为前 9 条可见会话显示带底色的 `⌘1`–`⌘9` 键帽。
4. **验证**：
   - 新增 `TestSessionRetitle`、`TestPromptSuggestionOnTurnEnd`（`kernel/session/title_suggest_test.go`）与 `TestRetitleSuggestionAndCmdHoldBadges`（`app/ergonomics_test.go`）。
   - `gofmt -l`、`CGO_ENABLED=0 go build ./...`、`go build ./...`、`GOOS=linux go build ./kernel/... ./cmd/stratumd`、`go vet ./...`、`go tool mygo vet .`、`go test ./...` 全部通过。

---

# 附录（原 `tasks/plan.md`、`tasks/perf.md`，2026-11-23 并入）

## 计划：性能、人体工学与 Apple 化重做

### Context

用户的要求：
- 把性能和易用性优化到极致，着重用户体验和细节，要符合「人体工学」。
- 尽量借鉴 Apple 的产品和 `apple-design` skill，UI/UX 做得像 Apple 自己的 App。

后续由 Antigravity（AGY）接手并维护本计划。

用户的决定：
- 品牌：已由 Talos 迁至 **Stratum**，仍保留自有品牌，不用 Anthropic 的颜色和字体。色板仍是 Mono，蓝色只给「活着的东西」，字体用系统字体。
- 界面图标：换成 SF Symbols，用 mygo 的 `plugins/sfsymbols` 插件绘制。系统里没有对应符号时，回退到 Lucide。**已完成。**
- **App 图标与品牌 logo：已完成（已推送 `f5dc575`）**。`app/icons/stratum.svg` 为三条错开圆角横条；`design/icon/Stratum.icns` 与 `resources/icon.png` 采用瓷白渐变底 + 放大版黑曜石三横条，保留深色款 `design/icon/Stratum-dark.icns`。
- 右侧面板的 Diff 和 Files 视图改用 `mygo.WebView`，在里面接入 `@pierre/diffs` 和 `@pierre/trees`。见阶段 5。

规则（来自 `tasks/handoff.md`）：
- 交互先照 Apple 自家 App 做：Finder、Mail、Notes、Xcode、Safari、Spotlight、系统设置。Apple 没有对应交互时，再照 Claude Code、Claude 桌面版、Codex、versta、opencode、Antigravity 做。不得自己发明。
- 不得复制 Apple 的图标、logo 或品牌素材。SF Symbols 只能在运行时由系统绘制，不得把导出的符号存进仓库。
- 只用假实现和演示环境（`scripts/demo.sh`、`STRATUM_DEMO`）。
- 用户说「提交」后才提交。

不在范围内：
- Hooks 行内编辑。
- 同步 `design/gaps.md`。
- 语音输入。
- 把设置改成独立窗口。

---

### 当前状态（2026-11-23）

| 阶段 | 状态 | 说明 |
|---|---|---|
| 0 基线 | 已完成（已推送） | `app/perf_bench_test.go`、`app/diff_bench_test.go`、`client/stream_bench_test.go`；数字在 `tasks/perf.md` |
| 1 性能 | 已完成（已推送） | 全量 5 项性能优化已完成，见阶段 1 |
| 2 键盘与焦点 | 已完成（已推送） | `KeepFocus`、快捷键、菜单栏、文字缩放、已用时间、失败重试 |
| 3 宽容与可预期 | 已完成（已推送） | ⌘Z 撤销归档、移除项目确认、会话附件、按会话滚动位置、项目分组折叠持久化 |
| 4 Apple 化与精简 | 已完成（已推送） | 4.1–4.3 及后续左下角 `Stratum` 浮层、输入框 Mail 式附带栏、白底图标全部已推送（截至 `f5dc575`） |
| 5 右侧面板接入 Pierre | 已完成（已推送） | 原生后备 + `mygo.WebView`（`@pierre/diffs` + `@pierre/trees` + `marked`）已接入并嵌入二进制 |

最近一次验证（`f5dc575`）：`(cd app/web && npm run typecheck)`、`go test ./...` 全部 20 个包、`go build ./cmd/stratum ./cmd/stratumd` 全部通过。

---

### 统一的设计基准（来自 apple-design，所有阶段遵守）

- **弹簧**：默认临界阻尼（阻尼比 1.0，响应 0.3–0.4s）；带动量手势允许回弹（阻尼比 ~0.8）。已在 `app/theme.go` 统一为 `springDefault`、`springSnappy`、`springMomentum`。
- **响应**：按下即反馈（`press()`），动画可中途打断并从当前值反向。
- **空间一致**：浮层与菜单从触发控件处长出；从哪进、从哪出。
- **材质**：侧栏用较重材质，按钮/输入框用较轻 Liquid Glass，浅色半透明层不得叠在另一浅色半透明层上。
- **字体排印**：字距随字号变化（20+ `-0.02em`，13–15 接近 `0`，11.5 以下 `+0.01em`）。
- **宽容**：误操作用撤销保护；仅不可逆破坏性操作弹确认框。
- **减法**：一个功能只放一个入口，已在设置页与 `⌘K` 里的选项不再占顶栏。

---

### 阶段 0：建立基线 — 已完成（已推送）

- 基准：`BenchmarkFrameTranscript`、`FrameReviewDiff`、`FrameSidebar`、`FrameApproval`、`NewFileDiff`、`StreamDeltas`。

---

### 阶段 1：性能（界面线程）

已完成（已推送）：
- 审批 diff 按 `ap.ID` 缓存；Review diff 改用 `ui.List`；大文件 diff 后台计算；流式输出改用 `strings.Builder`；审批等待期间停止重画；`openIn` 在后台运行。

已完成（已推送）：
1. **工作区刷新后不重算 diff**：[review.go](file:///Users/shqingda/Projects/stratum/app/review.go) 按 `diffHash`（SHA-256）校验内容，工作区刷新仅清除已移除路径，内容未变不重算 Myers/tokenize。
2. **历史记录**：[client.go](file:///Users/shqingda/Projects/stratum/client/client.go) 新增 `PreDecodeFrame` 在读取 goroutine 预解码 `EventPage`；[history.go](file:///Users/shqingda/Projects/stratum/client/history.go) `LoadOlder` 仅增量解码新页并前置追加，不再重放全量 `c.log`。
3. **⌘F 查找**：[find.go](file:///Users/shqingda/Projects/stratum/app/find.go) 按 `e.Key` 与内容长度缓存小写索引。
4. **启动**：[store.go](file:///Users/shqingda/Projects/stratum/kernel/store/store.go) 与 [search.go](file:///Users/shqingda/Projects/stratum/kernel/store/search.go) 将 FTS 回填移到后台并通过 `WaitSearchReady` 同步；`OpenTurnSessions` 单条 SQL 替代 [session.go](file:///Users/shqingda/Projects/stratum/kernel/session/session.go) `Recover` 的 N+1 查询。
5. **内存**：[lru.go](file:///Users/shqingda/Projects/stratum/app/lru.go) 通用 LRU 接入 `cardCache`（512）、`thumbs`（64）和 `rv.diffs`（64）。

---

### 阶段 2：键盘与焦点 — 已完成（已推送）

- `KeepFocus()` 保持输入框焦点；审批支持 `⌥⌘⏎` / `⌘⏎` / `⏎` / `esc`；运行中显示已用时间与 `esc 中断`；失败可重试、错误可关闭；菜单栏补齐、文字缩放 `⌘+`/`⌘−`/`⌘0`；`sessionOrder` 保持 `⌘1–9` 稳定；输入框内 `⇧⌘A` 照常归档。

---

### 阶段 3：宽容与可预期 — 已完成（已推送）

- `⌘Z` 撤销归档；移除项目前确认框；附件按会话保存；按会话记住滚动位置（`app.lists`）；侧栏项目分组折叠并持久化（`Settings.CollapsedProjects`）。

---

### 阶段 4：Apple 化的界面细节与布局精简

#### 4.1 & 4.2 已完成（部分未提交）
- SF Symbols 替换 Lucide；跟随系统「减少动态效果 / 降低透明度 / 增强对比度」；HUD VoiceOver 播报；≥24pt 点击区；Finder 顺序右键菜单与焦点环；统一空状态；窄窗口规则（先收侧栏再浮层显示 Review）；统一弹簧常量。
- **单卡片 Liquid Glass 输入框**：20pt 圆角 + `composerGlass` + 柔和焦点光晕 + 同心 `28×28` 发送按钮，去掉外部 `metaBar` 与内部项目胶囊 `folderPill`。
- **权限模式更名与去箭头**（2026-11-22 用户决定推翻「去箭头」：胶囊加回 `chevron.up.chevron.down`，权限胶囊仍不加图标；输入框加 Mail 式「附带」栏，不做双层托盘）：`ask` → 手动 / Manual，`auto` → 自动 / Auto，`full` → 完全访问 / Full access（红字高亮）；模型与权限胶囊均去掉下拉箭头与权限图标。
- **`@session` 纯芯片化**：`mentionSessions` 独立保存为可移除芯片，不在输入文本里塞 `@session:<id>`。
- **侧栏取消「需要你」分组，左下角重做为 `Stratum` 品牌栏与方案 B 浮层**：审批会话留在原项目下并保留蓝色 `Approval` 徽标；左下角改为 `brandMark` + `Stratum` + 额度环与百分比，点击向上弹出 `ui.Popover`（`p.Chip` 额度卡 + `设置… ⌘,`），去掉登录与多账号切换。
- **审批卡紧凑单行布局**：命令框上下两行防挤压、说明纵向排列，按钮精简为单行（左 `Edit command`，右 `Deny` · `Always Allow` `⌘⏎`（本会话）· `Allow` `⏎`）。
- **顶栏工作区胶囊重构**：`workspaceCapsule` 改为「项目」与「分支」统一 13pt 二级菜单；移除顶栏 `Detail`；「打开方式」仅显示本机已安装的应用（限定清单：Finder、Terminal、Ghostty、Warp、iTerm2、Cursor、VS Code 含 Insiders、Zed、Xcode、Sublime Text）；外跳按钮换为 `arrow.up.forward.app`，无匹配编辑器时自动回退 `open -t` / `open -R`，错误改走顶部 HUD 轻提示。

#### 4.3 已完成（已推送）
1. **VoiceOver 播报扩展**：[app.go](file:///Users/shqingda/Projects/stratum/app/app.go) `syncAnnounce` 在新审批到来和轮次结束时调用 `c.Announce`，切换会话同步基线不误报。
2. **主题切换渐变**：[theme.go](file:///Users/shqingda/Projects/stratum/app/theme.go) `PaletteLerp` 配合 `c.Root().Spring("theme", ...)` 在浅色和深色主题间平滑渐变，开启「减少动态效果」时直接跳变。

---

### 阶段 5：右侧面板接入 Pierre（`mygo.WebView`）

#### 5.1 原生现状（已完成，已推送）
- `codeSpans` 使用 `editor.DefaultTheme` 彩色高亮；`Files` 页签支持虚拟化可折叠目录树、异步代码预览、宽屏/最大化并排显示与收起目录树开关；`.md` 文件默认以渲染视图（`预览`）打开并支持 `预览 | 源码` 切换；右上角支持 `Maximize Pane` 全屏；`scripts/demo.sh` 已为 `calc`、`site`、`notes` 补齐 `docs/` 与 `internal/ops/` 嵌套目录。

#### 5.2 目标与步骤（已完成，已推送）
Diff 视图和 Files 视图改由 `mygo.WebView` 承载（原生实现保留为后备）：
1. **前提核实**：`@pierre/diffs`（`1.6.4`）与 `@pierre/trees`（`1.0.0`）均为 `Apache-2.0` 许可证；`mygo@v0.10.0` `NewWebView` / `ui.WebView` / `SetFrontend` / `BindAs` / `EmitPage` 已核对。
2. **前端包（[app/web/](file:///Users/shqingda/Projects/stratum/app/web/)）**：TypeScript + esbuild 打成单 JS + 单 CSS，由 [embed.go](file:///Users/shqingda/Projects/stratum/app/web/embed.go) `go:embed dist` 嵌入二进制（零网络请求）；字体使用系统 SF Pro / SF Mono，色板由 Go `Palette` 动态注入。
3. **桥接协议（[pierre.go](file:///Users/shqingda/Projects/stratum/app/pierre.go)）**：Go → 页面（`pierre:theme`、`pierre:mode`、`pierre:diffs`、`pierre:tree`、`pierre:file`）；页面 → Go（`Ready`、`SelectFile`、`CloseTab`、`ToggleTree`、`ToggleDiffStyle`、`ToggleMarkdownRaw`、`RevertFile`、`AddRemark`、`OpenExternal`、`EscapeToComposer`）。
4. **Files & Markdown & Diff 交互**：并排目录树 + 一键收起、多文件标签页、面包屑、`.md` 默认 `Preview | Raw` 切换、Diff 的 `Unified | Split` 双视图切换，无 WebView 环境（含 headless 测试）保留原生后备。
5. **焦点与键盘**：WebView 内 `⌘F` 查找、`Esc` 还焦点给输入框，窗口快捷键由 `mygo` 透传。
6. **性能基准**：已重跑并更新 [perf.md](file:///Users/shqingda/Projects/stratum/tasks/perf.md)。

---

### 验证

1. **每次改动后**：`go test ./...` 全部 20 个包通过（`TestQuotaAsyncRefresh` 偶发失败时单独重跑 `./llm/auth` 确认）；新增交互补 `ui.NewTester` 测试。
2. **真机清单（`scripts/demo.sh`）**：流式输出流畅度、2000 行 diff 滚动、Files 并排目录树与收起、Markdown `预览 | 源码` 切换、Maximize Pane、无「需要你」分组、顶栏项目/分支二级菜单、仅显示已安装外部打开应用、键盘全流程、760pt 窄窗口、辅助功能偏好与 VoiceOver。

## 性能基线与对比（不提交）

机器：本机 darwin，`-benchtime 20x`（流式 `5x`）。

| 基准 | 基线 ns/op | 基线 B/op | 基线 allocs/op | 优化后 ns/op | 优化后 B/op | 优化后 allocs/op |
|---|---|---|---|---|---|---|
| FrameTranscript（2000 条） | 328,448 | 807,516 | 2,198 | 323,021 | 811,226 | 2,198 |
| FrameReviewDiff（2000 行） | 273,800,008 | 441,162,712 | 625,283 | 13,387,588 | 8,489,200 | 14,089 |
| FrameSidebar（200 会话） | 519,217 | 182,262 | 691 | 490,040 | 182,262 | 691 |
| FrameApproval（1000 行改动） | 58,064,940 | 48,605,686 | 4,219 | 205,306 | 10,896 | 180 |
| StreamDeltas（1 万次增量，每次） | 197,184,500 | 1,440,870,896 | 23,454 | 3,535,292 | 1,707,182 | 10,044 |

| NewFileDiff（2 万行，一次） | 63,785,208 | 307,067,914 | 244,186 | 改到后台计算 | | |

说明：
- FrameReviewDiff 剩下的时间主要是无头光栅化（真机用 GPU），App 自己的构建约 2.7ms；真机 WebView 路径由 `@pierre/diffs` 虚拟化渲染。
- FrameTranscript 和 FrameSidebar 本来就低于 1ms，本轮没有改。
- 2026-11-22 阶段 5 复测（Apple M3 Pro）：
  - `FrameTranscript`：309,468 ns/op，813,297 B/op，2,288 allocs/op
  - `FrameReviewDiff`（原生后备）：16,551,208 ns/op，20,754,820 B/op，16,823 allocs/op
  - `FrameSidebar`：507,759 ns/op，186,485 B/op，827 allocs/op
  - `FrameApproval`：207,139 ns/op，14,541 B/op，254 allocs/op
  - `StreamDeltas`：2,251,016 ns/op，1,703,452 B/op，10,040 allocs/op
  - `NewFileDiff`（2 万行）：64,222,926 ns/op，309,052,780 B/op，244,202 allocs/op（已移至后台）
