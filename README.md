# Claude EPUB Reader

一个用 Go 写的终端 EPUB 阅读器：外观模拟 Claude Code 的终端界面，正文却是真实的 EPUB 内容。

```text
✻ Claude EPUB · 《终端里的 Claude》                              ▰▰▰▱▱▱▱▱ 33% · 2/3
────────────────────────────────────────────────────────────────────────────
  ✻ Claude  ·  正在阅读

  第二章 代码与高亮

  代码块会使用 Chroma 做语法高亮。长行会自动折行。

  ╭─ go
  │ package main
  │
  │ func main() {
  │     fmt.Println("Claude, but make it a book")
  │ }
  ╰─
────────────────────────────────────────────────────────────────────────────
  ✻ 输入 help 查看操作 · Enter 打开可见图片
  ❯ / 输入消息或命令                                                     浏览
  ↑↓ 滚动 · ←→ 章节 · Enter 图片 · t 目录 · f 收藏 · b 返回 · / 对话 · ? 帮助
```

> 这是 Claude 风格的非官方本地阅读工具，不冒充 Anthropic 产品；不联网，也不调用任何模型。

## 功能

- 纵向连续滚动阅读，章节边界自动衔接；上/下方向键和 PageUp/PageDown 翻动。
- 按终端宽度实时重新排版，代码、列表、图片缩略图都会重排。
- 小于 56 列 × 16 行时显示拉宽提示，恢复到阈值后回到原阅读位置。
- EPUB3 `nav` 与 EPUB2 `NCX` 目录；目录弹窗支持方向键快速选择、章节/锚点跳转。
- 跳转历史：任何目录/收藏跳转前都会记录位置，按 `b` 返回。
- 收藏分两种：
  - 精确阅读位置（章节 + 章内百分比）
  - 整个章节
- 收藏和最后阅读位置持久化；下次打开同一条路径时恢复。
- Chroma 语法高亮，支持行内代码、粗体、斜体、链接、列表、引用、表格。
- EPUB 内图片：
  - 正文中是 truecolor 半块字符缩略图；
  - 在 iTerm2/Kitty/Ghostty/WezTerm 中按 Enter 用终端原生协议打开高清图；
  - iTerm2 使用 OSC 1337 原始图片协议，不是字符画。
- 浏览模式和对话输入模式分离，单键快捷方式不会和打字冲突。
- 简单关键词对话命令，中英文都支持，不需要模型。

## 环境

- Go 1.26+（依赖的 x/image、Chroma 等要求）
- 推荐的终端：
  - **iTerm2**：原生图片、truecolor，当前首选
  - **WezTerm**：iTerm2 图片协议
  - **Kitty / Ghostty**：Kitty graphics protocol
- 普通终端仍可阅读和显示彩色缩略图，但不支持高清原图弹层。
- 当前版本未实现 tmux/screen 图片直通，建议直接在终端中运行。

## 构建与运行

```sh
make build
./bin/claude-epub testdata/demo.epub
```

或者：

```sh
go run . testdata/demo.epub
```

参数：

```text
--state-file <path>       自定义收藏/进度文件
--image-protocol <name>   auto | iterm2 | kitty | none
--version                 显示版本
```

状态文件默认位置：

```text
macOS: ~/Library/Application Support/claude-epub-reader/state.json
其他系统: os.UserConfigDir()/claude-epub-reader/state.json
```

也可用环境变量 `CLAUDE_EPUB_READER_STATE` 覆盖。

## 快捷键

默认是**浏览模式**。按 `/`、`:` 或 `i` 进入**输入模式**，Esc 回到浏览。

### 浏览模式

| 按键 | 功能 |
|---|---|
| `↑` / `↓`、`k` / `j` | 逐行滚动 |
| `PgUp` / `PgDn`、`Space` | 整页滚动 |
| `←` / `→`、`h` / `l` | 上一章 / 下一章 |
| `g` / `G`、`Home` / `End` | 章首 / 章尾 |
| `Enter` | 打开当前视口内的图片；没有图片时聚焦输入框 |
| `o` | 打开当前视口内的图片 |
| `t` | 目录弹窗 |
| `f` | 收藏/取消收藏当前精确位置 |
| `s` | 收藏/取消收藏当前章节 |
| `F` | 收藏列表 |
| `b` | 返回上一个跳转位置 |
| `/`、`:`、`i` | 进入输入模式 |
| `?`、`F1` | 帮助 |
| `q` | 退出 |
| `Ctrl+C` | 退出（任何模式） |

### 输入模式

| 按键 | 功能 |
|---|---|
| `Enter` | 提交命令 |
| `Esc` | 返回浏览 |
| `↑` / `↓` | 命令历史 |
| `Ctrl+U` | 清空输入 |

### 弹窗

- 目录/收藏：`↑↓` 选择，`PgUp`/`PgDn` 翻页，`Enter` 跳转，`Esc` 关闭。
- 目录中：`s` 收藏/取消收藏高亮章节。
- 收藏列表中：`d` 删除选中收藏。

## 对话命令

命令解析是关键词/正则匹配，不调用 LLM。支持可选 `/` 前缀以及中英文。

| 命令示例 | 功能 |
|---|---|
| `help`、`帮助`、`?` | 打开帮助 |
| `目录`、`toc`、`大纲` | 打开目录弹窗 |
| `下一章`、`next`、`n` | 下一章 |
| `上一章`、`prev`、`p` | 上一章 |
| `向下`、`向下滚动`、`page down` | 向下滚一页 |
| `向上`、`page up` | 向上滚一页 |
| `顶部`、`章首`、`top` | 当前章开头 |
| `底部`、`章尾`、`bottom` | 当前章末尾 |
| `跳到第三章`、`goto 3`、`第 3 章` | 按章节号跳转 |
| `收藏`、`收藏这里` | 切换当前位置收藏 |
| `收藏本章`、`收藏章节` | 切换整章收藏 |
| `收藏列表`、`favs` | 打开收藏列表 |
| `返回`、`back`、`b` | 回到上一个跳转位置 |
| `图片`、`查看图片` | 打开当前视口图片 |
| `图片 2`、`image 2` | 打开本章第 2 张图片 |
| `进度`、`status` | 显示位置与收藏数 |
| `退出`、`quit`、`exit` | 退出 |

## 图片说明

阅读页中的图片是 **truecolor 半块字符缩略图**（`▀`），会随宽度缩放。

按 Enter 后会：

1. 提取 EPUB 内原始图片数据；
2. 根据终端协议将其归一到合适的格式与传输体积；
3. 通过 `tea.ExecProcess` 暂停 TUI，启动同一个二进制的隐藏子命令 `__view-image`；
4. 子命令在独立 alt-screen 中用 iTerm2 OSC 1337 或 Kitty graphics 显示图片；
5. 按任意键退出，Bubble Tea 恢复并重绘阅读界面。

iTerm2 有约 1 MiB 的传输限制；图片准备阶段会优先复用原始 PNG/JPEG/GIF，必要时缩放并重新编码。当前支持 PNG、JPEG、GIF、WebP、BMP；SVG 暂不栅格化，会显示占位提示。

如果原图没有弹出：

```sh
./bin/claude-epub --image-protocol iterm2 testdata/demo.epub
```

并确认没有运行在 tmux/screen 中、终端支持对应图片协议。

## 响应式布局

- 宽度变化会重新折行所有内容，并按“章节 + 章内百分比”恢复阅读位置。
- 代码块长行会在保持缩进的前提下硬折行。
- 最小尺寸：**56 列 × 16 行**。低于该尺寸只显示拉宽提示，恢复后自动回到原阅读位置。
- 图片缩略图最大 48 列宽、14 行高，并保持原始宽高比。

## 安全与失败处理

- EPUB 内文本会移除 C0/C1 控制字符，防止 ANSI 转义注入。
- 不访问网络、不加载外部图片，不支持 DRM/加密 EPUB。
- ZIP 条目有大小限制；图片解码前检查尺寸和像素数；解码缓存有上限。
- 状态文件使用原子写入，权限 `0600`；损坏的 JSON 会在下次保存时备份为 `.corrupt`。
- 章节解析失败时会记录警告并尽量继续打开其余章节。

## 开发与测试

```sh
make fmt
make vet
make test
make build
```

重新生成示例书：

```sh
make sample
# 等价于 go run scripts/gen-demo-epub.go
```

示例书是原创测试内容，包含：

- EPUB3 nav + EPUB2 NCX
- 嵌套目录和锚点
- 中文排版、列表、引用
- Go/Python 代码块，含超长行
- 横向和竖向 PNG 图片
- 表格

测试覆盖 EPUB 解析、目录、路径解析、控制字符清洗、CJK/代码折行、宽度不变量、收藏持久化、协议序列和 TUI 状态机。

## 目录结构

```text
main.go                     CLI 与 __view-image 子命令分派
internal/epub/              ZIP/OPF/spine/XHTML/TOC 解析
internal/render/            响应式排版、Chroma 高亮、图片缩略图
internal/imgterm/           终端图片协议、图片准备、查看器
internal/state/             收藏与进度持久化
internal/tui/               Bubble Tea 模型、命令、弹窗、布局
internal/testepub/          示例 EPUB 生成器
scripts/gen-demo-epub.go    生成 testdata/demo.epub
testdata/demo.epub          手测样书
```
