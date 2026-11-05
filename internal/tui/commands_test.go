package tui

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		in   string
		kind ActionKind
		arg  int
	}{
		{"help", ActionHelp, 0},
		{"/help", ActionHelp, 0},
		{"帮助", ActionHelp, 0},
		{"?", ActionHelp, 0},
		{"about", ActionAbout, 0},
		{"关于", ActionAbout, 0},
		{"作者", ActionAbout, 0},
		{"目录", ActionTOC, 0},
		{"toc", ActionTOC, 0},
		{"下一章", ActionNextChapter, 0},
		{"next chapter", ActionNextChapter, 0},
		{"上一章", ActionPrevChapter, 0},
		{"下一节", ActionNextSection, 0},
		{"下一小节", ActionNextSection, 0},
		{"next section", ActionNextSection, 0},
		{"上一节", ActionPrevSection, 0},
		{"prev section", ActionPrevSection, 0},
		{"下一页", ActionPageDown, 0},
		{"向下滚动", ActionPageDown, 0},
		{"上一页", ActionPageUp, 0},
		{"章首", ActionTop, 0},
		{"章尾", ActionBottom, 0},
		{"收藏", ActionTogglePositionFavorite, 0},
		{"收藏这里", ActionTogglePositionFavorite, 0},
		{"收藏本章", ActionToggleChapterFavorite, 0},
		{"收藏列表", ActionFavorites, 0},
		{"返回", ActionBack, 0},
		{"跳到第三章", ActionGotoChapter, 3},
		{"goto 12", ActionGotoChapter, 12},
		{"去第25章。", ActionGotoChapter, 25},
		{"图片 2", ActionOpenImage, 2},
		{"image 3", ActionOpenImage, 3},
		{"查看图片", ActionOpenImage, 0},
		{"进度", ActionStatus, 0},
		{"退出", ActionQuit, 0},
		{"认真阅读", ActionUnknown, 0},
	}
	for _, tt := range tests {
		got := ParseCommand(tt.in)
		if got.Kind != tt.kind {
			t.Errorf("ParseCommand(%q).Kind = %v, want %v", tt.in, got.Kind, tt.kind)
		}
		if tt.arg != 0 && got.Image != tt.arg && got.Chapter != tt.arg {
			t.Errorf("ParseCommand(%q) arg = chapter %d image %d, want %d", tt.in, got.Chapter, got.Image, tt.arg)
		}
	}
}

func TestChineseNumbers(t *testing.T) {
	cases := map[string]int{
		"一": 1, "十": 10, "十二": 12, "二十": 20, "二十三": 23,
		"一百": 100, "一百零三": 103, "两百": 200, "三百二十一": 321,
	}
	for in, want := range cases {
		if got := parseChineseNumber(in); got != want {
			t.Errorf("parseChineseNumber(%q) = %d, want %d", in, got, want)
		}
	}
}
