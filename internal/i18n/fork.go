package i18n

// ForkMessages are the texts of this fork's own UI additions. They live in one
// file so an upstream rebase never meets them: a fork-only file cannot
// conflict, the way internal/update/fork.go keeps the fork's release machinery
// out of the merge. The Chinese and Traditional tables follow the side-catalog
// naming (chineseFork, traditionalFork) so tools/zhtw/gen.py renames the
// reference when it regenerates the Traditional catalog from the Simplified
// one; traditionalFork's wording mirrors what the generator's term list
// produces.

// ForkMessages holds the fork-only surfaces' strings: the terminal status bar
// and the compact-density choice.
type ForkMessages struct {
	// The status bar's settings row and its empty state.
	StatusBarRow          string
	StatusBarDescription  string
	StatusBarNoSession    string
	StatusBarLogColorHint string

	// Connection phases the bar names while a session is not connected yet.
	StatusBarConnecting     string
	StatusBarAuthenticating string

	// The tool rail's fold button and handle (toolbar_rail_fold.go).
	RailFoldHint   string
	RailExpandHint string

	// The compact-density choice row.
	CompactDensityRow         string
	CompactDensityDescription string
	CompactDensityStandard    string
	CompactDensityCompact     string
}

var englishFork = ForkMessages{
	StatusBarRow:          "Status bar",
	StatusBarDescription:  "Show a bar under the terminals with the focused session's connection, mode and latency. Off by default.",
	StatusBarNoSession:    "No session",
	StatusBarLogColorHint: "Device log coloring",

	StatusBarConnecting:     "Connecting…",
	StatusBarAuthenticating: "Authenticating…",

	RailFoldHint:   "Collapse the tool rail",
	RailExpandHint: "Expand the tool rail",

	CompactDensityRow:         "Compact interface",
	CompactDensityDescription: "Tighter spacing and smaller controls across the window, so more terminals fit on one screen.",
	CompactDensityStandard:    "Standard",
	CompactDensityCompact:     "Compact",
}

var chineseFork = ForkMessages{
	StatusBarRow:          "状态栏",
	StatusBarDescription:  "在终端下方显示一条状态栏，呈现当前会话的连接、模式与延迟。默认关闭。",
	StatusBarNoSession:    "无会话",
	StatusBarLogColorHint: "设备日志着色",

	StatusBarConnecting:     "连接中…",
	StatusBarAuthenticating: "认证中…",

	RailFoldHint:   "收起工具栏",
	RailExpandHint: "展开工具栏",

	CompactDensityRow:         "紧凑界面",
	CompactDensityDescription: "整体收紧间距与控件尺寸，让一屏容纳更多终端。",
	CompactDensityStandard:    "标准",
	CompactDensityCompact:     "紧凑",
}

var traditionalFork = ForkMessages{
	StatusBarRow:          "狀態列",
	StatusBarDescription:  "在終端機下方顯示一條狀態列，呈現目前工作階段的連線、模式與延遲。預設關閉。",
	StatusBarNoSession:    "無工作階段",
	StatusBarLogColorHint: "設備記錄著色",

	StatusBarConnecting:     "連線中…",
	StatusBarAuthenticating: "驗證中…",

	RailFoldHint:   "收起工具列",
	RailExpandHint: "展開工具列",

	CompactDensityRow:         "緊湊介面",
	CompactDensityDescription: "整體收緊間距與控制項尺寸，讓一個螢幕容納更多終端機。",
	CompactDensityStandard:    "標準",
	CompactDensityCompact:     "緊湊",
}
