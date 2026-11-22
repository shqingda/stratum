# Stratum 设计规范

方向：**单色 Mono**（2026-11-12 选定）。与 ogma 同族：共用色板，各有标志。

## 原则

- 按钮、选中、边框只用 Ink / Paper 的中性色。
- **蓝色只给「活着的东西」**：运行中、等你处理、流式输出的光标、进度。界面上蓝色越少，它越像信号。
- 没有渐变、发光和彩色阴影。层级靠字重、字号、材质（侧栏毛玻璃、浮层更亮）来区分。
- 动效默认用临界阻尼的弹簧；只有带动量的手势才回弹。浮层从触发它的控件处长出。

## 图标

应用标识为**三条错开的圆角横条（地层 / Stratum）**：

- 应用内单色符号：[app/icons/stratum.svg](file:///Users/shqingda/Projects/stratum/app/icons/stratum.svg)（`24×24` 网格，`currentColor`，随浅/深色文字自适应）。
- macOS 应用图标：连续超椭圆（`1024×1024` 画布，`824×824` tile，圆角 `186`）。默认采用瓷白渐变底 + 黑曜石三横条（`Stratum.icns` 与 `resources/icon.png`），同时保留深黑曜石底款（`Stratum-dark.icns`）。
- 16 / 32 px 小尺寸使用独立光学简化版，保持像素级清晰度。

文件在 `icon/`：

| 文件 | 说明 |
|---|---|
| `src/stratum.py` | 生成浅/深色主图与 32 / 16 px 简化版 SVG |
| `build.sh` | 渲染并打包 `Stratum.icns`、`Stratum-dark.icns` 与 `resources/icon.png` |
| `stratum-icon-light{,-32,-16}.svg` | 瓷白底默认款 SVG（1024 / 32 / 16） |
| `stratum-icon-dark{,-32,-16}.svg` | 深黑底保留款 SVG（1024 / 32 / 16） |
| `Stratum.icns` / `Stratum-dark.icns` | macOS 应用图标包 |

## 颜色

| 名称 | 浅色 | 深色 | 用途 |
|---|---|---|---|
| bg | `#F4F5F7` Paper | `#12131A` Ink | 窗口底 |
| side | `#EAECF0` | `#171922` | 侧栏（毛玻璃底色） |
| card | `#FFFFFF` | `#1C1E28` | 卡片、输入框、浮层 |
| fg | `#12131A` | `#E8EAEF` | 正文 |
| fg2 | `#5B6272` Slate | `#A9B1C1` Mist | 次要文字 |
| mute | `#8A91A0` | `#6E7587` | 提示、时间、占位 |
| edge | Ink 12% | Mist 11% | 发丝线 |
| sel | Ink 6% | Mist 8% | 选中、悬停 |
| acc | `#12131A` | `#E8EAEF` | 主按钮（文字用 bg 色） |
| live | `#2B5FA6` Ogma Blue | `#7AA8EA` Night | 活着的东西 |
| plus / minus | `#3B7356` / `#A8474B` | `#7DB89A` / `#DD878B` | Diff 增删 |

对比度：Ogma Blue 不要放在 Ink 上，Night 不要放在 Paper 上。

## 字

- 界面用系统字体（SF Pro、苹方），代码用 SF Mono。数字一律等宽。
- 字号：正文 13.5，控件 12.5，辅助 11.5，标题 15 / 20。
- 字重：400 正文，500 用户消息与按钮，600 标题，650 大标题。大字号收紧字距（-0.015em）。

## 样稿

- `brand-directions.html`：品牌方向的比较（已选 C）。
- `mockups.html`：高保真样稿，包括主窗口与 Review、⌘K、设置、首次登录。
