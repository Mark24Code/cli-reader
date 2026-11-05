package testepub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"
)

type Options struct {
	IncludeNav bool
	IncludeNCX bool
}

func DefaultOptions() Options {
	return Options{IncludeNav: true, IncludeNCX: true}
}

// BuildSample returns a valid EPUB3/EPUB2 hybrid sample book. It is original
// content written for tests and the manual demo.
func BuildSample(opts Options) ([]byte, error) {
	wide, err := gradientPNG(640, 360, color.RGBA{R: 42, G: 75, B: 110, A: 255}, color.RGBA{R: 240, G: 144, B: 96, A: 255})
	if err != nil {
		return nil, err
	}
	tall, err := gradientPNG(480, 720, color.RGBA{R: 30, G: 60, B: 48, A: 255}, color.RGBA{R: 220, G: 214, B: 180, A: 255})
	if err != nil {
		return nil, err
	}

	var extraManifest strings.Builder
	if opts.IncludeNav {
		extraManifest.WriteString(`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav" />` + "\n")
	}
	if opts.IncludeNCX {
		extraManifest.WriteString(`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml" />` + "\n")
	}
	spineAttr := ""
	if opts.IncludeNCX {
		spineAttr = ` toc="ncx"`
	}

	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:claude-epub-reader-demo</dc:identifier>
    <dc:title>终端里的 Claude</dc:title>
    <dc:creator>Mark24</dc:creator>
    <dc:language>zh-CN</dc:language>
  </metadata>
  <manifest>
    <item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml" />
    <item id="ch2" href="ch2.xhtml" media-type="application/xhtml+xml" />
    <item id="ch3" href="ch3.xhtml" media-type="application/xhtml+xml" />
    <item id="wide" href="images/wide.png" media-type="image/png" />
    <item id="tall" href="images/tall.png" media-type="image/png" />
    <item id="css" href="style.css" media-type="text/css" />
    %s
  </manifest>
  <spine%s>
    <itemref idref="ch1" />
    <itemref idref="ch2" />
    <itemref idref="ch3" />
  </spine>
</package>
`, extraManifest.String(), spineAttr)

	nav := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>目录</title></head>
<body>
  <nav epub:type="toc">
    <h1>目录</h1>
    <ol>
      <li><a href="ch1.xhtml">第一章 开篇</a>
        <ol><li><a href="ch1.xhtml#why">为什么在终端读书</a></li></ol>
      </li>
      <li><a href="ch2.xhtml">第二章 代码与高亮</a>
        <ol><li><a href="ch2.xhtml#syntax">语法高亮</a></li></ol>
      </li>
      <li><a href="ch3.xhtml">第三章 排版与图片</a></li>
    </ol>
  </nav>
</body>
</html>`

	ncx := `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head><meta name="dtb:uid" content="urn:uuid:claude-epub-reader-demo"/></head>
  <docTitle><text>终端里的 Claude</text></docTitle>
  <navMap>
    <navPoint id="n1" playOrder="1"><navLabel><text>第一章 开篇</text></navLabel><content src="ch1.xhtml"/>
      <navPoint id="n1a" playOrder="2"><navLabel><text>为什么在终端读书</text></navLabel><content src="ch1.xhtml#why"/></navPoint>
    </navPoint>
    <navPoint id="n2" playOrder="3"><navLabel><text>第二章 代码与高亮</text></navLabel><content src="ch2.xhtml"/></navPoint>
    <navPoint id="n3" playOrder="4"><navLabel><text>第三章 排版与图片</text></navLabel><content src="ch3.xhtml"/></navPoint>
  </navMap>
</ncx>`

	codeGo := `package main

import "fmt"

// 一段故意很长的代码行，用来验证宽度变化时不会把行折坏。
func main() {
    message := "Claude, but make it a book"
    fmt.Println(message)
    for i := 0; i < 3; i++ {
        fmt.Printf("reading chapter %d\n", i)
    }
}`
	codeLong := `const veryLongLine = "This line is deliberately long so the renderer must soft-wrap it while preserving syntax colors and indentation for a narrow terminal window."`
	codePy := `def greet(name: str) -> str:
    """给读者一个问候。"""
    return f"你好，{name}！"

print(greet("终端读者"))`

	ch1 := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>第一章 开篇</title><link rel="stylesheet" href="style.css"/></head>
<body>
  <section>
    <h1>第一章 开篇</h1>
    <p>这是一本只在终端里阅读的小书。它的正文不是聊天记录，而是 EPUB 里的真实章节；界面看起来却像是 Claude 正在回答你。</p>
    <p>向下滚动就能继续读，本章结束后会自动进入下一章。<strong>粗体</strong>、<em>斜体</em>、<code>行内代码</code> 都会尽量保留原有层次。</p>
    <h2 id="why">为什么在终端读书</h2>
    <ul>
      <li>不用离开键盘，也不打断心流。</li>
      <li>目录、收藏、图片和代码都在同一个界面里。</li>
      <li>深色终端里，Claude 橙会成为恰到好处的注脚。</li>
    </ul>
    <blockquote><p>阅读不是把字符搬到屏幕上，而是让注意力停留在正确的地方。</p></blockquote>
    <p>如果窗口太窄，程序会提醒你把终端拉宽；拉宽后，正文会自动重排。</p>
  </section>
</body>
</html>`

	ch2 := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>第二章 代码与高亮</title><link rel="stylesheet" href="style.css"/></head>
<body>
  <section>
    <h1>第二章 代码与高亮</h1>
    <p>代码块会使用 Chroma 做语法高亮。长行会自动折行，而不是把内容丢到屏幕外。</p>
    <h2 id="syntax">语法高亮</h2>
    <p>下面的 Go 程序就是一段普通的正文，而不是截图：</p>
    <pre><code class="language-go">` + html.EscapeString(codeGo) + `</code></pre>
    <p>再来一段故意超过窗口宽度的代码：</p>
    <pre><code class="language-go">` + html.EscapeString(codeLong) + `</code></pre>
    <p>Python 也能识别：</p>
    <pre><code class="language-python">` + html.EscapeString(codePy) + `</code></pre>
    <figure>
      <img src="images/wide.png" alt="一幅横向渐变色图片，用于测试缩略图宽度"/>
      <figcaption>横向图片：按 Enter 可以用 iTerm2 原生协议查看高清大图。</figcaption>
    </figure>
    <p>代码之后，继续向下滚动。也可以顺着 <a href="ch1.xhtml#why">这个链接回到第一章的说明</a>，或者跳到 <a href="#syntax">本节的锚点</a>。</p>
  </section>
</body>
</html>`

	ch3 := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>第三章 排版与图片</title><link rel="stylesheet" href="style.css"/></head>
<body>
  <section>
    <h1>第三章 排版与图片</h1>
    <p>这个章节同时有竖图、表格和很长的中文段落，用来观察响应式重排。</p>
    <img src="images/tall.png" alt="一幅竖向渐变色图片"/>
    <p>中文折行应当自然，不应该因为词之间没有空格就把整段挤出屏幕。终端宽度改变时，阅读位置会按章内百分比近似恢复，所以重新打开窗口不会直接把你送回章首。</p>
    <table>
      <tr><th>功能</th><th>入口</th></tr>
      <tr><td>目录</td><td>t</td></tr>
      <tr><td>收藏</td><td>f / s</td></tr>
      <tr><td>返回</td><td>b</td></tr>
    </table>
    <p>最后一页。你可以按 <code>G</code> 到章尾，或输入“目录”回到目录弹窗。</p>
  </section>
</body>
</html>`

	container := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

	css := `body { font-family: serif; line-height: 1.6; }
.code { white-space: pre-wrap; }`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte, method uint16) error {
		h := &zip.FileHeader{Name: name, Method: method}
		h.SetModTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := add("mimetype", []byte("application/epub+zip"), zip.Store); err != nil {
		return nil, err
	}
	addFile := func(name, content string) error {
		return add("OEBPS/"+name, []byte(content), zip.Deflate)
	}
	if err := add("META-INF/container.xml", []byte(container), zip.Deflate); err != nil {
		return nil, err
	}
	for _, item := range []struct {
		name, content string
	}{
		{"content.opf", opf},
		{"ch1.xhtml", ch1},
		{"ch2.xhtml", ch2},
		{"ch3.xhtml", ch3},
		{"style.css", css},
	} {
		if err := addFile(item.name, item.content); err != nil {
			return nil, err
		}
	}
	if opts.IncludeNav {
		if err := addFile("nav.xhtml", nav); err != nil {
			return nil, err
		}
	}
	if opts.IncludeNCX {
		if err := addFile("toc.ncx", ncx); err != nil {
			return nil, err
		}
	}
	if err := add("OEBPS/images/wide.png", wide, zip.Deflate); err != nil {
		return nil, err
	}
	if err := add("OEBPS/images/tall.png", tall, zip.Deflate); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteSample(path string, opts Options) error {
	data, err := BuildSample(opts)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func gradientPNG(w, h int, from, to color.RGBA) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			tx := float64(x) / float64(max(1, w-1))
			ty := float64(y) / float64(max(1, h-1))
			t := (tx + ty) / 2
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(float64(from.R)*(1-t) + float64(to.R)*t),
				G: uint8(float64(from.G)*(1-t) + float64(to.G)*t),
				B: uint8(float64(from.B)*(1-t) + float64(to.B)*t),
				A: 0xff,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
