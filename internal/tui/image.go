package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cli-epub-reader/internal/imgterm"
	"cli-epub-reader/internal/render"
)

func (m *Model) findImage(index int) *render.ImageSpan {
	if m.content == nil || len(m.content.Images) == 0 {
		return nil
	}
	if index > 0 {
		for i := range m.content.Images {
			if m.content.Images[i].Index == index {
				return &m.content.Images[i]
			}
		}
		return nil
	}
	return m.content.VisibleImage(m.viewport.YOffset, m.viewport.Height)
}

func (m *Model) openImage(index int) tea.Cmd {
	if m.protocol == imgterm.ProtocolNone {
		m.setNotice("当前终端不支持原生图片；阅读页仍保留彩色缩略图", true)
		return nil
	}
	img := m.findImage(index)
	if img == nil {
		if index > 0 {
			m.setNotice(fmt.Sprintf("本章没有第 %d 张图片", index), true)
		} else {
			m.setNotice("当前视口内没有图片", true)
		}
		return nil
	}
	if img.Ref.Path == "" {
		m.setNotice("这张图片没有可读取的本地数据", true)
		return nil
	}
	data, _, err := m.book.ImageBytes(img.Ref.Path)
	if err != nil {
		m.setNotice("读取图片失败: "+err.Error(), true)
		return nil
	}
	prepared, err := imgterm.PrepareImage(data, m.protocol)
	if err != nil {
		m.setNotice("准备高清图片失败: "+err.Error(), true)
		return nil
	}
	f, err := os.CreateTemp("", "cli-epub-reader-image-*.img")
	if err != nil {
		m.setNotice("创建临时图片失败: "+err.Error(), true)
		return nil
	}
	tmp := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(tmp)
		m.setNotice("写入临时图片失败: "+err.Error(), true)
		return nil
	}
	if _, err := f.Write(prepared.Data); err != nil {
		f.Close()
		os.Remove(tmp)
		m.setNotice("写入临时图片失败: "+err.Error(), true)
		return nil
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		m.setNotice("写入临时图片失败: "+err.Error(), true)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		os.Remove(tmp)
		m.setNotice("无法定位当前程序: "+err.Error(), true)
		return nil
	}
	title := strings.TrimSpace(img.Ref.Alt)
	if title == "" {
		title = filepath.Base(img.Ref.Path)
	}
	ch := m.book.Chapters[m.chapter]
	title = ch.Title + " · " + title
	cmd := exec.Command(exe, "__view-image",
		"--file", tmp,
		"--title", title,
		"--protocol", string(m.protocol),
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	m.viewerBusy = true
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		_ = os.Remove(tmp)
		return viewerDoneMsg{Err: err}
	})
}
