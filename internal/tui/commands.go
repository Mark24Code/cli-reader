package tui

import (
	"regexp"
	"strconv"
	"strings"
)

type ActionKind int

const (
	ActionNone ActionKind = iota
	ActionUnknown
	ActionHelp
	ActionTOC
	ActionNextChapter
	ActionPrevChapter
	ActionGotoChapter
	ActionPageDown
	ActionPageUp
	ActionTop
	ActionBottom
	ActionTogglePositionFavorite
	ActionToggleChapterFavorite
	ActionFavorites
	ActionBack
	ActionOpenImage
	ActionStatus
	ActionQuit
)

type Action struct {
	Kind    ActionKind
	Chapter int // 1-based
	Image   int // 1-based, 0 means visible image
	Raw     string
}

var (
	reGotoCN  = regexp.MustCompile(`(?:第|跳到|跳转到|跳转|去|到)\s*([0-9]+|[一二三四五六七八九十百千零两]+)\s*[章节回]?`)
	reGotoEN  = regexp.MustCompile(`\b(?:goto|go\s+to|chapter|ch)\s+([0-9]+|[a-z]+)\b`)
	reImageCN = regexp.MustCompile(`(?:图片|图)\s*([0-9]+)`)
	reImageEN = regexp.MustCompile(`\b(?:image|img)\s*([0-9]+)\b`)
)

func ParseCommand(raw string) Action {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Action{Kind: ActionNone, Raw: raw}
	}
	norm := raw
	norm = strings.TrimPrefix(norm, "/")
	norm = strings.TrimPrefix(norm, ":")
	norm = strings.TrimSpace(norm)
	lower := strings.ToLower(norm)
	trimmed := strings.TrimRight(lower, " \t。.!！?？")
	action := Action{Raw: raw}

	if lower == "?" || lower == "？" || lower == "help" || lower == "帮助" || lower == "说明" ||
		lower == "快捷键" || strings.Contains(lower, "怎么用") || strings.Contains(lower, "help me") {
		action.Kind = ActionHelp
		return action
	}
	if containsAny(lower, "收藏列表", "我的收藏", "所有收藏", "favorites", "favourites", "bookmarks", "favs") {
		action.Kind = ActionFavorites
		return action
	}
	if containsAny(lower, "收藏本章", "收藏章节", "收藏这一章", "收藏整章", "fav chapter", "favorite chapter", "bookmark chapter") {
		action.Kind = ActionToggleChapterFavorite
		return action
	}
	if containsAny(lower, "收藏这里", "收藏此处", "收藏位置", "收藏当前", "收藏这页", "记录位置",
		"fav here", "fav position", "bookmark here", "bookmark position") ||
		lower == "fav" || lower == "favorite" || lower == "bookmark" ||
		(containsAny(lower, "收藏", "标记") && !containsAny(lower, "取消", "删除")) {
		action.Kind = ActionTogglePositionFavorite
		return action
	}
	if containsAny(lower, "收藏", "书签", "fav", "bookmark") {
		// "取消收藏" and similar are handled by the favorites modal; treat a
		// bare cancellation command as a position toggle.
		action.Kind = ActionTogglePositionFavorite
		return action
	}
	if containsAny(lower, "下一页", "向下翻", "向下滚", "往下滚", "往下翻", "往下", "向下", "下滚",
		"page down", "pagedown", "scroll down", "next page") ||
		lower == "down" || lower == "j" {
		action.Kind = ActionPageDown
		return action
	}
	if containsAny(lower, "上一页", "向上翻", "向上滚", "往上滚", "往上翻", "往上", "向上", "上滚",
		"page up", "pageup", "scroll up", "previous page") ||
		lower == "up" || lower == "k" {
		action.Kind = ActionPageUp
		return action
	}
	if containsAny(lower, "章首", "章节开头", "顶部", "开头", "回到顶部", "top", "beginning") ||
		lower == "g" {
		action.Kind = ActionTop
		return action
	}
	if containsAny(lower, "章尾", "章节末尾", "底部", "结尾", "到底部", "bottom", "end") ||
		lower == "G" {
		action.Kind = ActionBottom
		return action
	}
	if containsAny(lower, "下一章", "下章", "后一章", "下一节", "next chapter", "next chap") || lower == "n" || lower == "next" {
		action.Kind = ActionNextChapter
		return action
	}
	if containsAny(lower, "上一章", "上章", "前一章", "上一节", "previous chapter", "prev chapter", "last chapter") ||
		lower == "p" || lower == "prev" || lower == "previous" {
		action.Kind = ActionPrevChapter
		return action
	}
	if m := reImageCN.FindStringSubmatch(lower); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			action.Kind = ActionOpenImage
			action.Image = n
			return action
		}
	}
	if m := reImageEN.FindStringSubmatch(lower); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			action.Kind = ActionOpenImage
			action.Image = n
			return action
		}
	}
	if containsAny(lower, "查看图片", "打开图片", "看图", "当前图片", "open image", "show image", "view image") ||
		lower == "image" || lower == "img" {
		action.Kind = ActionOpenImage
		return action
	}
	if containsAny(lower, "目录", "章节列表", "大纲", "table of contents", "contents", "toc") || lower == "t" {
		action.Kind = ActionTOC
		return action
	}
	if containsAny(lower, "返回上次", "返回上一", "回到上次", "回到刚才", "后退", "返回", "回去", "back") ||
		lower == "b" {
		action.Kind = ActionBack
		return action
	}
	if containsAny(lower, "进度", "状态", "在哪", "status", "progress") {
		action.Kind = ActionStatus
		return action
	}
	if containsAny(lower, "退出", "结束阅读", "关闭程序", "quit", "exit") || lower == "q" {
		action.Kind = ActionQuit
		return action
	}
	if m := reGotoCN.FindStringSubmatch(trimmed); len(m) == 2 {
		if n := parseChapterNumber(m[1]); n > 0 {
			action.Kind = ActionGotoChapter
			action.Chapter = n
			return action
		}
	}
	if m := reGotoEN.FindStringSubmatch(lower); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			action.Kind = ActionGotoChapter
			action.Chapter = n
			return action
		}
		if n := parseChineseNumber(m[1]); n > 0 {
			action.Kind = ActionGotoChapter
			action.Chapter = n
			return action
		}
	}
	action.Kind = ActionUnknown
	return action
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func parseChapterNumber(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return parseChineseNumber(s)
}

func parseChineseNumber(s string) int {
	if s == "" {
		return 0
	}
	digits := map[rune]int{
		'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	}
	units := map[rune]int{'十': 10, '百': 100, '千': 1000}
	total := 0
	section := 0
	number := 0
	seen := false
	for _, r := range s {
		if d, ok := digits[r]; ok {
			number = d
			seen = true
			continue
		}
		if u, ok := units[r]; ok {
			if number == 0 {
				number = 1
			}
			section += number * u
			number = 0
			seen = true
			continue
		}
		return 0
	}
	if number > 0 {
		section += number
	}
	total += section
	if !seen {
		return 0
	}
	return total
}
