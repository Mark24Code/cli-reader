# cli-reader

一个用 Go 写的终端电子书阅读器：外观模拟 Claude Code 的终端界面，支持 EPUB、PDF、TXT、Markdown、HTML、CBZ 漫画，以及 Kindle 的 MOBI / AZW3。

```text
✻ cli-reader · 《终端里的 Claude》                                 ▰▰▰▱▱▱▱▱ 33% · 2/3
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
  ↑↓ 滚动 · ←→ 小节 · Enter 图片 · t 目录 · f 收藏 · b 返回 · / 对话 · ? 帮助
```

> 这是 Claude 风格的非官方本地阅读工具，不冒充 Anthropic 产品；不联网，也不调用任何模型。

## 支持的格式

| 格式 | 扩展名 | 阅读方式 | v1 边界 |
|---|---|---|---|
| EPUB | `.epub` | spine 章节、EPUB3 nav / EPUB2 NCX 目录、内嵌图片 | 不支持 DRM、JS、CSS 布局 |
| PDF | `.pdf` | 默认全屏原生图片模式（精确栅格化，打开即显示，无需回车）；可切换文本模式：提取文本层重排、识别代码和公式、内嵌图片按 EPUB 方式显示 | 图片模式需要本机渲染工具和原生图片终端；文本模式不还原原版式；扫描版无 OCR |
| TXT | `.txt`、`.text` | BOM / chardet 编码检测；中英文章节识别；无标题时按长度分节 | 不支持图片 |
| Markdown | `.md`、`.markdown`、`.mdown`、`.mkd` | GFM（表格、任务列表、删除线、footnote）、代码高亮、本地图片 | 单文件单章 + 标题目录；跨文件链接不跳转 |
| HTML | `.html`、`.htm`、`.xhtml`、`.xht` | 标题目录、表格、代码、本地图片 | 单文件单章；不执行脚本/CSS |
| CBZ | `.cbz`、图片 `.zip` | 每张图片一页，整页渲染；Enter 打开原生高清页；读取 ComicInfo.xml | 仅 ZIP；不支持 RAR/7z、OCR、SVG 页 |
| MOBI / AZW3 | `.mobi`、`.azw3`、`.azw`、`.prc` | PalmDOC / HUFF-CDIC 解压、EXTH 元数据、内嵌图片、标题目录 | 仅无 DRM 文件；KF8/AZW3 为尽力解析 |

> 所有格式都只支持无 DRM 的内容。遇到加密书籍会明确提示，不会尝试解密。

## 功能

- 纵向连续滚动阅读，章节/页边界自动衔接；方向键和 PageUp/PageDown 翻动。
- 支持鼠标：滚轮翻页；点击正文链接、脚注锚点或目录项跳转；按住左键拖动选中文本，松开自动复制。
- 按终端宽度实时重新排版，代码、列表、图片缩略图都会重排。
- 小于 56 列 × 16 行时显示拉宽提示，恢复后回到原阅读位置。
- 目录弹窗：EPUB nav/NCX、TXT 章节、Markdown/HTML 标题、PDF/CBZ 页码、MOBI 标题。
- 跳转历史：任何目录/收藏跳转前都会记录位置，按 `b` 返回。
- 收藏分两种：精确阅读位置（章节 + 章内百分比）和整个章节。
- 收藏和最后阅读位置持久化；下次打开同一条路径时恢复。
- Chroma 语法高亮：Markdown/HTML/EPUB 代码块，以及 PDF 中按等宽字体/缩进识别的代码。
- 图片：
  - 正文中是 truecolor 半块字符缩略图；
  - CBZ 页和扫描版 PDF 页按整页缩略图显示；
  - iTerm2/Kitty/Ghostty/WezTerm 中按 Enter 用终端原生协议打开高清图；
  - Markdown/HTML 允许引用书文件同目录子树内的本地图片，越界路径会被拒绝。
- 浏览模式和对话输入模式分离，单键快捷方式不会和打字冲突。
- 内置「关于」页面（`a`），展示版本、作者与项目主页。
- 简单关键词对话命令，中英文都支持，不需要模型。

## 环境

- Go 1.26+（依赖的 x/image、Chroma、pdfcpu 等要求）
- 推荐的终端：
  - **iTerm2**：原生图片、truecolor，当前首选
  - **WezTerm**：iTerm2 图片协议
  - **Kitty / Ghostty**：Kitty graphics protocol
- 普通终端仍可阅读和显示彩色缩略图，但不支持高清原图弹层。
- 发布构建使用 `CGO_ENABLED=0`，纯 Go，可交叉编译。
- PDF 图片模式需要本机渲染工具：macOS 自带 `sips`；其他平台可用 `pdftoppm`（poppler）、`mutool`、`gs`（Ghostscript）或 ImageMagick。找不到工具时会提示并回退到文本模式。
- 当前版本未实现 tmux/screen 图片直通，建议直接在终端中运行。

## 构建与运行

```sh
make build
./bin/cli-reader testdata/demo.epub
./bin/cli-reader novel.txt
./bin/cli-reader manual.pdf              # 默认图片模式
./bin/cli-reader --pdf-mode text manual.pdf
./bin/cli-reader comic.cbz
./bin/cli-reader book.azw3
```

或者：

```sh
go run . testdata/demo.epub
```

参数：

```text
--state-file <path>       自定义收藏/进度文件
--image-protocol <name>   auto | iterm2 | kitty | none
--format <name>           auto | epub | pdf | txt | md | html | cbz | mobi | azw3
--pdf-mode <name>         image（默认，精确图片渲染）| text | auto
--version                 显示版本
```

`--format` 默认 `auto`。自动检测顺序：

1. ZIP 魔数：含 `META-INF/container.xml` 或 EPUB `mimetype` → EPUB；含图片条目 → CBZ。
2. `%PDF-` → PDF。
3. PalmDB 类型 `BOOK`/`MOBI`（或 `TEXt`/`REAd`）→ MOBI/AZW3。
4. 扩展名判断 TXT / Markdown / HTML / CBZ / PDF / MOBI。
5. 未知扩展名但内容像文本 → TXT；否则报错。

扩展名与魔数冲突时以魔数为准。

状态文件默认位置：

```text
~/.cli-reader/state.json
```

也可用 `--state-file <path>` 或环境变量 `CLI_READER_STATE` 覆盖。旧环境变量 `CLI_EPUB_READER_STATE` 仍然兼容。旧版本的 `~/.cli-epub-reader/state.json` 和系统配置目录下的状态文件会在首次使用新路径且新文件不存在时自动复制过来；旧文件保留不删。

### 平台构建与 Release

GitHub Actions 配置在 `.github/workflows/release.yml`：

- 推送到 `main`：运行 `go vet`、`CGO_ENABLED=0 go test`，并在 Linux / macOS / Windows 上分别构建 `amd64`、`arm64` 二进制，产物保存在本次 Action 的 Artifacts 中。
- 推送 `v*` tag：测试通过后由 GoReleaser 构建全部平台，并自动创建 GitHub Release，附带 `tar.gz`（Windows 为 `zip`）、`README.md`、示例书和 `checksums.txt`。

```sh
git tag v0.4.0
git push origin v0.4.0
```

发布流程会把 tag 版本注入 `cli-reader --version`。

### 发布命令

```sh
make release                 # 交互式：显示当前版本并询问新版本
make release VERSION=minor   # 非交互：patch / minor / major 或 1.2.3
./scripts/release.sh patch   # 等价的脚本入口
```

`release` 会依次：

1. 显示当前版本，并允许输入 `patch` / `minor` / `major` 或完整的 `x.y.z`（直接回车默认 patch）；
2. 运行 `go vet ./...` 和全部测试，失败则中止；
3. 更新 `internal/buildinfo/buildinfo.go` 中的版本号；
4. 提交当前所有改动，commit 信息为 `release: vX.Y.Z`；
5. 创建并推送 `vX.Y.Z` tag，触发 GitHub Actions 构建并发布 Release。

## 快捷键

默认是**浏览模式**。按 `/`、`:` 或 `i` 进入**输入模式**，Esc 回到浏览。

### 浏览模式

| 按键 | 功能 |
|---|---|
| `↑` / `↓`、`k` / `j` | 逐行滚动 |
| `PgUp` / `PgDn`、`Space` | 整页滚动 |
| `←` / `→`、`h` / `l` | 上一节 / 下一节（按目录条目） |
| `n` / `p` | 下一阅读单元 / 上一个（EPUB 章、TXT 节、PDF/CBZ 页） |
| `g` / `G`、`Home` / `End` | 当前单元开头 / 末尾 |
| `Enter` | 打开当前视口内的图片；没有图片时聚焦输入框 |
| `o` | 打开当前视口内的图片 |
| `t` | 目录弹窗 |
| `f` | 收藏/取消收藏当前精确位置 |
| `s` | 收藏/取消收藏当前章节 |
| `F` | 收藏列表 |
| `b` | 返回上一个跳转位置 |
| `/`、`:`、`i` | 进入输入模式 |
| `Tab` | PDF：文本模式 ↔ 全屏原生图片阅读器 |
| `m` | 同 `Tab`（PDF 模式切换别名） |
| `?`、`F1` | 帮助 |
| `a` | 关于（作者与版本信息） |
| `q` | 退出 |
| `Ctrl+C` | 退出（任何模式） |

### PDF 全屏图片阅读器

| 按键 | 功能 |
|---|---|
| `↑` / `↓`、`j` / `k` | 纵向滚动画布（每次约 20% 屏高） |
| `PgUp` / `PgDn`、`Space` | 整屏滚动 |
| `←` / `→`、`n` / `p` | 上一页 / 下一页 |
| `-` / `=` | 缩小 / 放大（25%–400%，默认 100% 撑满宽度） |
| `g` / `G`、`Home` / `End` | 页首 / 页尾 |
| `0` / `z` | 回到页首 / 重置缩放 |
| `t` | 目录（页码列表） |
| `/` 或 `:` | 输入页码跳转 |
| `b` | 返回上一页 |
| `?` | 帮助 |
| `Tab` | 切换到文本模式 |
| `q` / `Esc` / `Ctrl+C` | 退出 |

### 鼠标

| 操作 | 功能 |
|---|---|
| 滚轮上 / 下 | 向上 / 向下滚动 3 行 |
| 按住左键拖动 | 选中屏幕文本，松开后自动复制 |
| 左键单击正文链接 | 跳转到目标章节或章内锚点 |
| 左键点击目录/收藏条目 | 选中并跳转 |
| 左键点击弹窗外部 | 关闭弹窗 |
| 弹窗内滚轮 | 滚动目录、收藏或帮助列表 |

内置链接跳转只在书内生效：外部 URL、目标不在阅读内容中、以及 KF8 的 `kindle:pos` / `filepos` 链接会在提示栏说明，不会联网打开。启用鼠标后，终端里直接拖动被程序用于选择文本；复制优先使用系统剪贴板，不可用时回退到 OSC 52。按住 Shift 拖动仍可使用终端原生选择。

### 输入模式

| 按键 | 功能 |
|---|---|
| `Enter` | 提交命令 |
| `Esc` | 返回浏览 |
| `↑` / `↓` | 命令历史 |
| `Ctrl+U` | 清空输入 |

### 弹窗

- 目录/收藏：`↑↓` 选择，`PgUp`/`PgDn` 翻页，`Enter` 跳转，`Esc` 关闭；也可用鼠标点击条目或点击弹窗外部关闭。
- 目录中：`s` 收藏/取消收藏高亮章节。
- 收藏列表中：`d` 删除选中收藏。

## 对话命令

命令解析是关键词/正则匹配，不调用 LLM。支持可选 `/` 前缀以及中英文。

| 命令示例 | 功能 |
|---|---|
| `help`、`帮助`、`?` | 打开帮助 |
| `about`、`关于`、`作者` | 打开关于页面 |
| `目录`、`toc`、`大纲` | 打开目录弹窗 |
| `下一章`、`next`、`n` | 下一个阅读单元 |
| `上一章`、`prev`、`p` | 上一个阅读单元 |
| `下一节`、`next section` | 下一节目录条目 |
| `上一节`、`prev section` | 上一节目录条目 |
| `向下`、`向下滚动`、`page down` | 向下滚一页 |
| `向上`、`page up` | 向上滚一页 |
| `顶部`、`章首`、`top` | 当前单元开头 |
| `底部`、`章尾`、`bottom` | 当前单元末尾 |
| `跳到第三章`、`goto 3`、`第 3 章` | 按编号跳转 |
| `收藏`、`收藏这里` | 切换当前位置收藏 |
| `收藏本章`、`收藏章节` | 切换整章收藏 |
| `收藏列表`、`favs` | 打开收藏列表 |
| `返回`、`back`、`b` | 回到上一个跳转位置 |
| `图片`、`查看图片` | 打开当前视口图片 |
| `图片 2`、`image 2` | 打开本章第 2 张图片 |
| `图片模式`、`pdf image` | 进入 PDF 全屏原生图片阅读器 |
| `文本模式`、`pdf text` | 切换到 PDF 文本模式 |
| `进度`、`status` | 显示位置与收藏数 |
| `退出`、`quit`、`exit` | 退出 |

## 图片说明

阅读页中的图片是 **truecolor 半块字符缩略图**（`▀`），会随宽度缩放。PDF 内嵌图片、MOBI 内嵌图片、CBZ 页面和本地 Markdown/HTML 图片都走同一套渲染。

PDF 的**图片模式**是例外：它不使用字符缩略图，而是打开专用的全屏原生图片阅读器，直接用 iTerm2 / Kitty 图片协议显示整页 PNG；只有文本模式下的内嵌图片才使用缩略图 + Enter 查看。

按 Enter 后会：

1. 从书容器或本地目录提取原始图片数据；
2. 根据终端协议将其归一到合适的格式与传输体积；
3. 通过 `tea.ExecProcess` 暂停 TUI，启动同一个二进制的隐藏子命令 `__view-image`；
4. 子命令在独立 alt-screen 中用 iTerm2 OSC 1337 或 Kitty graphics 显示图片；
5. 按任意键退出，Bubble Tea 恢复并重绘阅读界面。

iTerm2 有约 1 MiB 的传输限制；图片准备阶段会优先复用原始 PNG/JPEG/GIF，必要时缩放并重新编码。当前支持 PNG、JPEG、GIF、WebP、BMP、TIFF；SVG 和 JPEG 2000 暂不栅格化，会显示占位提示。

如果原图没有弹出：

```sh
./bin/cli-reader --image-protocol iterm2 testdata/demo.epub
```

并确认没有运行在 tmux/screen 中、终端支持对应图片协议。

## 格式细节

### PDF

- **默认图片模式**：打开 PDF 后直接进入全屏原生图片阅读器，无需先看缩略图或按 Enter。每页由本机渲染器精确栅格化为 PNG 并用 iTerm2 / Kitty 原生协议显示。
- 页面默认**按终端宽度撑满**显示；页面高于阅读区时，`↑`/`↓`（或 `j`/`k`）按约 **20% 屏高**纵向滚动。滚动时不整屏清屏，避免闪烁。
- 图片阅读器提供导航条（顶部进度条 + 页码，底部状态栏），按键与文本模式保持一致：`←`/`→` 或 `n`/`p` 翻页，`↑`/`↓` 或 `j`/`k` 滚动，`-`/`=` 缩小/放大，`g`/`G` 页首/页尾，`t` 打开页码目录，`/` 输入页码跳转，`b` 返回上一页，`?` 帮助，`Tab` 切换到文本模式，`q` 退出。窗口大小变化会自动重新适配。
- macOS 优先使用系统 `sips`（配合 pdfcpu 抽取单页）；其他平台依次尝试 `pdftoppm`、`mutool`、`gs`、ImageMagick。找不到任何工具或终端不支持原生图片协议时提示并回退文本模式。
- 在文本模式按 `Tab`（或 `m`、输入 `图片模式`）会重新进入全屏原生图片阅读器；`--pdf-mode text` 可直接以文本模式启动。
- 文本模式优先使用页面文本层重建段落；行间大间距作为分段依据，CJK 与拉丁文字使用不同拼接规则。
- 字体名包含 `Mono`、`Courier`、`Consolas`、`Menlo`、`Monaco`、`Source Code`、`Fira Code`、`CMTT`、`SFMono` 等的行会识别为代码块，并按原始 X 坐标还原缩进；没有字体信息时用缩进和符号密度兜底。
- 代码块交给 Chroma 自动识别语言并高亮。
- 每页一章，目录按页生成。
- 自动按字体统计乱码率，剔除没有可用 ToUnicode 的水印/隐藏文字层；整页不可读时跳过文字并按图片显示。
- 识别常见的双列表格（短标签列 + 宽内容列），按单元格坐标重建为终端表格；复杂的多栏、合并单元格和跨页表格仍按文本流重排。
- 单行线性公式会按 X 坐标重排，并把小字号、基线偏移的字符还原为下标/上标（如 `w₁x₁ + w₂x₂`）；复杂分式、分段函数和矩阵仍按文本流显示。
- 内嵌图片通过纯 Go 的 pdfcpu 抽取；CMYK TIFF 也可解码。图片追加到该页正文之后；没有文字、只有图片的页按整页图片显示。
- 不还原多栏版式、页眉页脚、表格和公式；扫描版依赖图片页，不做 OCR。
- 加密/需要密码的 PDF 会作为 DRM 拒绝。

### TXT

- 编码顺序：BOM → UTF-8 校验 → chardet → 常见东亚编码尝试 → Windows-1252 兜底。
- 章节标题识别：`第X章/节/回/卷/篇/部`、`Chapter/Part/Book`、`前言/序章/后记/尾声`、Markdown 风格 `#` 标题。
- 没有标题时按约 1200 行或 64 KiB 在空行处分节。
- 识别引用（`>`）、列表、缩进代码块；组内换行按 CJK 规则合并。

### Markdown / HTML

- Markdown 使用 goldmark + GFM（表格、删除线、任务列表、footnote），再交给与 EPUB 共用的 HTML → Block 解析器。
- 支持 front matter 的 `title` / `author` / `language`。
- 单文件单章，标题生成目录锚点。
- 本地图片只允许书文件同目录子树内的相对路径；绝对路径、`..` 越界、外部 URL、`data:` 图片会被拒绝或标记为外部。
- 脚本、样式、`<head>` 等非正文内容会被丢弃。

### CBZ

- 自然排序（`page2` 排在 `page10` 前），忽略 `__MACOSX`、隐藏文件和非图片。
- 每张图片一页；可读取 `ComicInfo.xml` 的 Title / Series / Number / Writer / LanguageISO。
- 整页缩略图铺满阅读区，Enter 打开原生高清页。
- 仅支持 ZIP 容器；加密 ZIP 作为 DRM 拒绝。

### MOBI / AZW3

- 解析 PalmDB、PalmDOC 头、MOBI 头和 EXTH 元数据。
- 支持无压缩、PalmDOC LZ77、HUFF/CDIC 解压。
- MOBI7 和 KF8/AZW3 都按文本记录读取；混合书优先读取前半部分。
- `recindex` / `kindle:embed` 图片映射到内嵌图片记录。
- 按标题拆分章节并生成目录；`filepos` / `kindle:pos` 位置链接暂不跳转。
- 仅支持无 DRM 文件；固定版式 KF8 只按文本流读取。

## 响应式布局

- 宽度变化会重新折行所有内容，并按“当前阅读单元 + 单元内百分比”恢复阅读位置。
- 代码块长行会在保持缩进的前提下硬折行。
- 最小尺寸：**56 列 × 16 行**。低于该尺寸只显示拉宽提示，恢复后自动回到原阅读位置。
- 普通图片缩略图最大 48 列宽、14 行高；CBZ/扫描页按阅读区宽高铺满。

## 安全与失败处理

- 书内文本会移除 C0/C1 控制字符，防止 ANSI 转义注入。
- 不访问网络、不加载外部图片；Markdown/HTML 本地图片限制在书文件同目录子树内。
- 加密 EPUB / PDF / MOBI / CBZ 会识别为 DRM 并拒绝，不做任何解密。
- ZIP 条目、PDF 页数/图片、MOBI 记录/解压结果都有大小上限；图片解码前检查尺寸和像素数；解码缓存有上限。
- 状态文件使用原子写入，权限 `0600`；损坏的 JSON 会在下次保存时备份为 `.corrupt`。
- 单个章节/页面解析失败时会记录警告并尽量继续打开其余内容。
- PDF 的文本/图片抽取和 MOBI 解压都包裹了 `recover`，畸形输入会报错而不是崩溃。

## 开发与测试

```sh
make fmt
make vet
make test
make build
CGO_ENABLED=0 go test ./...
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
- 表格（对齐网格、表头样式、colspan/rowspan 合并单元格、窄屏降级布局）

测试覆盖：

- EPUB 解析、目录、路径解析、控制字符清洗、DRM/字体混淆
- HTML → Block 解析器
- TXT 编码检测、章节识别、CJK 拼接、代码块
- Markdown GFM、front matter、本地图片、HTML 元数据
- CBZ 自然排序、ComicInfo、整页渲染
- PDF 文本/代码/图片/加密/截断
- MOBI PalmDOC、HUFF/CDIC、EXTH、图片、DRM、截断，以及 AZW3 写入器生成的样例
- 格式检测、渲染宽度不变量、收藏持久化、TUI 状态机

## 目录结构

```text
main.go                     CLI、--format 与 __view-image 子命令分派
internal/buildinfo/         版本与作者信息
internal/book/              格式无关的书模型、图片 Provider、TOC 工具
internal/htmlimport/        HTML/XHTML → Block 解析器
internal/textdecode/        BOM/chardet/x-text 编码解码
internal/formats/           格式检测与 loader 分发
internal/epub/              EPUB 解析器（ZIP/OPF/spine/NCX）
internal/txtbook/           TXT 解析器
internal/mdbook/            Markdown 解析器
internal/htmlbook/          单文件 HTML 解析器
internal/cbzbook/           CBZ 漫画解析器
internal/pdfbook/           PDF 文本/代码/图片解析器
internal/mobibook/          MOBI/AZW3 解析器（PalmDOC/HUFF-CDIC/EXTH）
internal/render/            响应式排版、Chroma 高亮、图片缩略图
internal/imgterm/           终端图片协议、图片准备、查看器
internal/state/             收藏与进度持久化
internal/tui/               Bubble Tea 模型、命令、弹窗、布局
internal/testepub/          示例 EPUB 生成器
scripts/gen-demo-epub.go    生成 testdata/demo.epub
scripts/release.sh          版本递增、提交、打 tag 并推送
testdata/demo.epub          手测样书
```

## 作者

- **Mark24Code**
- 邮箱：mark.zhangyoung@gmail.com
- 主页：https://github.com/Mark24Code/cli-reader
