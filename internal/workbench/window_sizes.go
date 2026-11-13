package workbench

// MinimumWindowWidth and MinimumWindowHeight keep the workbench's navigation,
// titlebar controls and main content usable, in logical (DPI-independent) pixels.
// At the minimum height the sidebar shows its header at full spacing and every
// row down to Settings; a new row must raise the minimum rather than compress
// the header. Viva reduces a minimum only when the display's available work area
// is smaller.
//
// The fork's minimum is upstream's 620 plus 20: this sidebar carries one more
// entry (the HTTP file server), and at full header spacing its last row ends
// 3 px past 620 — the raise keeps upstream's own margin between the rows' end
// and the window's bottom. Below 640 the header gives up its gap instead, as
// the stress tests that paint a deliberately small window require.
const (
	MinimumWindowWidth  = 960
	MinimumWindowHeight = 640

	remoteToolsMinWidth  = 960
	remoteToolsMinHeight = 600
	sftpWindowMinWidth   = 900
	sftpWindowMinHeight  = 560
	fileWindowMinWidth   = 640
	fileWindowMinHeight  = 420
)
