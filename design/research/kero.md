# Kero

来源：kero.sh 的文档（Shortcuts、Status & Notifications、Sessions & Projects、Configuration、Changes、Panes）和 GitHub README。Kero 是给 coding agent 用的终端，和 Talos 一样用 mygo 原生界面写，源码 MIT 可读；本机装了，可以直接看真实界面。它没有自己的对话流和输入框，每个分栏就是一个真终端，跑 agent 自己的 TUI。所以对话相关的部分不适用，值得看的是侧栏、状态、改动面板和通知。

## 布局

- 左侧栏：项目和会话。
- 中间：会话里的一个或多个分栏，每个是终端或浏览器。
- 右侧栏两个页：**Changes** 和 **Files**（⌥⌘B 开合）。从这两页打开的文件，以卡片形式出现在分栏旁边。
- Quick Terminal：从屏幕顶部下拉，盖在别的应用上面。

## 侧栏与状态

- 会话归在项目下面。每行最前面是这个会话里那个程序的图标（Claude Code、Codex、Gemini、Node、普通终端）。**agent 在跑时图标有颜色，shell 回到提示符后变淡。**
- 分栏状态五种：working、waiting、idle、error、exited。
- 分了栏的会话，名字后面显示分栏数。分支名和领先/落后数悬停时才显示。
- 程序可以用 `kero report` 上报一句状态，显示在会话名下面和分栏标题里。
- **done 和 error 的标记一直留着，直到你在那个分栏里打字**——结果会等你。
- 项目可拖动排序。侧栏行有 detailed / compact 两种密度。
- 双击侧栏空白处新建会话。把文件夹拖到侧栏上，每个文件夹开一个会话。

## 新会话

- ⌘T 新会话，⌘⇧T 在 Home 新建，⌘⌥T 在 worktree 里新建（问分支名，放在 `.kero/worktrees/〈分支〉`），⌘O 打开项目。
- 关闭 worktree 会话时问「Remove Worktree」还是「Keep Worktree」：**没有未提交改动时默认删，有未提交改动时默认留。**

## Changes 面板

- 三个范围：**Last Turn / Uncommitted / Branch**。
- 所有 diff 叠在一个滚动区里，滚到哪个文件，文件列表里就高亮哪个。
- Last Turn 里悬停一处改动出「Revert」和「Keep」。会话里有 agent 在工作时不能 Revert。
- 「Collapse All」折叠全部。diff 布局 unified / split / auto，可设是否换行。

## 分栏

- ⌘D 右分，⌘⇧D 下分，⌘⇧↩ 放大当前分栏，⌘] / ⌘[ 切换，⌘⌥方向键按方向移动焦点。
- 关闭正在跑程序的分栏先确认（默认开）。

## 通知

- 三个开关：等你时、做完时、出错时，默认全开。声音默认关，Dock 角标默认开（显示需要你的会话数），Dock 弹跳默认关。
- 只在你没在看的时候发。

## 设置

- 设置窗口（⌘,）编辑的是一个纯文本配置文件，手改也会自动重载。
- 分页：General、Appearance、Terminal、Shell、Notifications、Agents、Hosts、Git、Browser、Keyboard。
- 主题：跟随系统 / 浅 / 深；可写成 `light:paper,dark:dusk` 这样一对。⌘⇧L 切换深色。

## 快捷键

| 作用 | 键 |
|---|---|
| 跳到下一个需要你的会话 | ⌘J |
| 命令面板（Go to Session…） | ⌘P |
| 新会话 / 打开项目 | ⌘T / ⌘O |
| 改名 / 关闭会话 | ⌘R / ⌘⇧W |
| 上/下一个会话 | ⌘⇧[ / ⌘⇧] |
| 第 N 个会话 | ⌘1–9 |
| 侧栏 | ⌘B |
| Changes / Files | ⌥⌘G / ⌥⌘E |
| 字号 | ⌘= / ⌘- / ⌘0 |
| 退出但会话继续跑 / 退出并结束全部 | ⌘Q / ⌘⌥Q |

## 值得抄的细节

- ⌘J 直接跳到下一个在等你的会话。
- 图标本身表达「是哪个 agent」和「是否在跑」，不另加状态点。
- 完成和出错的标记留到你真的去处理为止。
- 关闭对话框的默认按钮随 worktree 是否干净而变。
- 退出应用不结束会话；重启后恢复会话并接上各 agent 的对话。
