package imgterm

import (
	"os"
	"strings"
)

type Protocol string

const (
	ProtocolNone   Protocol = "none"
	ProtocolITerm2 Protocol = "iterm2"
	ProtocolKitty  Protocol = "kitty"
)

// DetectProtocol determines the best available inline-image protocol.
func DetectProtocol() Protocol {
	return DetectFrom(os.Getenv)
}

func DetectFrom(getenv func(string) string) Protocol {
	termProgram := strings.ToLower(getenv("TERM_PROGRAM"))
	lcTerminal := strings.ToLower(getenv("LC_TERMINAL"))
	term := strings.ToLower(getenv("TERM"))

	switch {
	case strings.Contains(lcTerminal, "iterm2"),
		termProgram == "iterm.app",
		termProgram == "wezterm":
		return ProtocolITerm2
	case getenv("KITTY_WINDOW_ID") != "",
		strings.Contains(term, "kitty"),
		termProgram == "ghostty":
		return ProtocolKitty
	}
	return ProtocolNone
}

func (p Protocol) Label() string {
	switch p {
	case ProtocolITerm2:
		return "iTerm2 原生图片"
	case ProtocolKitty:
		return "Kitty graphics"
	default:
		return "无原生图片支持"
	}
}
