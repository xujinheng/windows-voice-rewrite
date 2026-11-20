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

// Holds is the sole cross-platform owner of physical key press generations.
// OS adapters handle hooks and timers, never another business hold state.
type keyState struct {
	down, active bool
	token        uint64
}

type Holds struct {
	keys  []keyState
	owner int
}

func NewHolds(count int) Holds { return Holds{keys: make([]keyState, count)} }

func (s *Holds) key(code int) *keyState {
	if code <= 0 || code >= len(s.keys) {
		return nil
	}
	return &s.keys[code]
}

func (s *Holds) Down(code int) bool {
	h := s.key(code)
	return h != nil && h.down
}

func (s *Holds) Press(code int, busy bool) uint64 {
	h := s.key(code)
	if h == nil || h.down || busy || s.owner != 0 {
		return 0
	}
	h.token++
	h.down, h.active = true, false
	s.owner = code
	return h.token
}

func (s *Holds) CanActivate(code int, token uint64) bool {
	h := s.key(code)
	return h != nil && s.owner == code && h.down && !h.active && h.token == token
}

func (s *Holds) Activate(code int, token uint64) bool {
	if !s.CanActivate(code, token) {
		return false
	}
	s.keys[code].active = true
	return true
}

// One release decision is shared by Windows and macOS. Native hosts only
// execute the returned effect (finish recording or replay a navigation click).
const (
	ReleaseIgnored = iota // unknown/unheld key, pass through
	ReleaseOther          // abandoned/secondary key, preserve navigation
	ReleaseActive         // confirmed voice hold, finish recording
	ReleaseShort          // armed but not activated, cancel and replay navigation
)

func (s *Holds) Release(code int) int {
	h := s.key(code)
	if h == nil || !h.down {
		return ReleaseIgnored
	}
	primary := s.owner == code
	if primary {
		s.owner = 0
	}
	active := h.active
	h.down, h.active = false, false
	h.token++
	if active {
		return ReleaseActive
	}
	if primary {
		return ReleaseShort
	}
	return ReleaseOther
}

func (s *Holds) Abandon()    { s.owner = 0 }
func (s *Holds) Armed() bool { return s.owner != 0 }

// Cancel invalidates every late timer without resetting its generation.
func (s *Holds) Cancel() {
	s.owner = 0
	for i := range s.keys {
		if s.keys[i].down {
			s.keys[i].down, s.keys[i].active = false, false
			s.keys[i].token++
		}
	}
}
