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

// Holds gives both native hosts one exclusive physical-key owner. The per-key
// generation prevents delayed activation from resurrecting a released press.
type Holds struct {
	keys  []Hold
	owner int
}

func NewHolds(count int) Holds { return Holds{keys: make([]Hold, count)} }

func (s *Holds) key(code int) *Hold {
	if code <= 0 || code >= len(s.keys) {
		return nil
	}
	return &s.keys[code]
}

func (s *Holds) Down(code int) bool {
	h := s.key(code)
	return h != nil && h.Down()
}

func (s *Holds) Press(code int, busy bool) uint64 {
	h := s.key(code)
	if h == nil || busy || s.owner != 0 {
		return 0
	}
	token, ok := h.Press()
	if !ok {
		return 0
	}
	s.owner = code
	return token
}

func (s *Holds) CanActivate(code int, token uint64) bool {
	h := s.key(code)
	return h != nil && s.owner == code && h.CanActivate(token)
}

func (s *Holds) Activate(code int, token uint64) bool {
	h := s.key(code)
	return h != nil && s.owner == code && h.Activate(token)
}

func (s *Holds) Release(code int) (held, active, primary bool) {
	h := s.key(code)
	if h == nil {
		return false, false, false
	}
	held, active = h.Release()
	primary = s.owner == code
	if primary {
		s.owner = 0
	}
	return
}

func (s *Holds) Abandon()    { s.owner = 0 }
func (s *Holds) Armed() bool { return s.owner != 0 }

// Cancel invalidates every late timer without resetting its generation.
func (s *Holds) Cancel() {
	s.owner = 0
	for i := range s.keys {
		s.keys[i].Release()
	}
}
