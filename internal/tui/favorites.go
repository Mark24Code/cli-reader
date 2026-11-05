package tui

import (
	"fmt"
	"strings"

	"cli-epub-reader/internal/state"
)

func (m *Model) togglePositionFavorite() {
	if m.content == nil {
		return
	}
	ch := m.book.Chapters[m.chapter]
	percent := m.currentPercent()
	added := m.bookState.TogglePositionFavorite(ch.Href, ch.Title, percent)
	m.syncLast()
	if err := m.store.Save(); err != nil {
		m.setNotice("收藏保存失败: "+err.Error(), true)
		return
	}
	if added {
		m.setNotice(fmt.Sprintf("已收藏当前位置 · %s %.0f%%", ch.Title, percent*100), false)
	} else {
		m.setNotice("已取消该位置收藏", false)
	}
}

func (m *Model) toggleChapterFavorite() {
	if m.content == nil {
		return
	}
	ch := m.book.Chapters[m.chapter]
	added := m.bookState.ToggleChapterFavorite(ch.Href, ch.Title)
	m.syncLast()
	if err := m.store.Save(); err != nil {
		m.setNotice("收藏保存失败: "+err.Error(), true)
		return
	}
	if added {
		m.setNotice("已收藏章节 · "+ch.Title, false)
	} else {
		m.setNotice("已取消章节收藏", false)
	}
}

func (m *Model) favoriteTitle(f state.Favorite) string {
	title := strings.TrimSpace(f.ChapterTitle)
	if title == "" {
		title = f.Href
	}
	return title
}
