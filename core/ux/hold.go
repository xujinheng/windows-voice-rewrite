package ux

import "time"

const (
	// ApplySettle is the pause after focusing the editor and before replacement.
	// It is not the 35ms copy settle.
	ApplySettle = 90 * time.Millisecond

	// These three sentences stay distinct from the copy and paste window checks.
	// Each still contains 窗口已, so Present classifies it as a moved selection.
	HoldWindowChanged  = "编辑窗口已经变化，取消自动替换"
	HoldSelectionMoved = "选区所属窗口已变化"
	ApplyWindowClosed  = "原编辑窗口已关闭"
)
