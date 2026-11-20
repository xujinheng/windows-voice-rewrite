package input

import "time"

const (
	// NoTargetWindow is the keyboard hold failure. A side-button arm with no
	// foreground window returns without this sentence.
	NoTargetWindow = "没有可用的目标窗口"
	// ToggleUINotifyFailed is shown when the toggle worker cannot reach the UI thread.
	ToggleUINotifyFailed = "无法通知 UI 线程准备录音"
)

// HoldModeLabel is the recording pill. Meeting dictation and meeting rewrite stay distinct.
func HoldModeLabel(dictation, meeting bool) string {
	switch {
	case meeting && dictation:
		return "会议直译"
	case meeting:
		return "会议意图"
	case dictation:
		return "听写"
	default:
		return "改写"
	}
}

const (
	// WindowsSideHold is how long Mouse4 or Mouse5 must stay down before recording starts.
	WindowsSideHold = 250 * time.Millisecond
)

// Hold is one physical button's press, hold, and release.
// A late timer carries the old token and cannot start a newer press.
type Hold struct {
	down   bool
	active bool
	token  uint64
}

func (s *Hold) Down() bool { return s.down }

func (s *Hold) Press() (uint64, bool) {
	if s.down {
		return s.token, false
	}
	s.token++
	s.down = true
	s.active = false
	return s.token, true
}

func (s *Hold) CanActivate(token uint64) bool {
	return s.down && !s.active && s.token == token
}

func (s *Hold) Activate(token uint64) bool {
	if !s.CanActivate(token) {
		return false
	}
	s.active = true
	return true
}

func (s *Hold) Release() (wasHeld, active bool) {
	if !s.down {
		return false, false
	}
	active = s.active
	s.down = false
	s.active = false
	s.token++
	return true, active
}
