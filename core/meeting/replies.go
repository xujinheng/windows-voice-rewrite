package meeting

// ReplyBatch contains three independently streaming suggestions and a shared
// hint. It is immutable once passed to ReplyState.Publish.
type ReplyBatch struct {
	Lines  [3]Line
	Source string
	Tip    string
}

// FirstReadyReply selects the first independently completed, copyable
// suggestion. A provider may omit ONE but return TWO or THREE successfully.
func FirstReadyReply(lines [3]Line) int {
	for i, line := range lines {
		if ReplyCopyEnabled(line.Complete, line.English) {
			return i
		}
	}
	return -1
}

func (b ReplyBatch) HasReadyReply() bool {
	return FirstReadyReply(b.Lines) >= 0
}

// Apply incorporates one protocol line into the in-progress batch.
func (b *ReplyBatch) Apply(line Line) bool {
	switch line.Kind {
	case "ONE":
		b.Lines[0] = line
	case "TWO":
		b.Lines[1] = line
	case "THREE":
		b.Lines[2] = line
	case "TIP":
		b.Tip = line.Chinese
	default:
		return false
	}
	return true
}

// ReplyState owns the displayed/pending handoff and generation fencing.
// Callers synchronize access when updates arrive from multiple goroutines.
type ReplyState struct {
	Generation       uint64
	DisplaySerial    uint64
	PendingSerial    uint64
	Displayed        ReplyBatch
	Pending          ReplyBatch
	PendingAvailable bool
}

// Next starts a new generation; a superseded pending batch is no longer
// actionable, while a previously readable batch stays on screen.
func (s *ReplyState) Next() uint64 {
	s.Generation++
	s.Pending = ReplyBatch{}
	s.PendingSerial = 0
	s.PendingAvailable = false
	return s.Generation
}

func (s *ReplyState) Current(serial uint64) bool {
	return serial != 0 && serial == s.Generation
}

// Publish updates the visible batch when it is the first response or belongs
// to the accepted generation. Otherwise a new completed batch is offered to
// the user without replacing the reply they are reading.
func (s *ReplyState) Publish(serial uint64, batch ReplyBatch) bool {
	if !s.Current(serial) {
		return false
	}
	if !s.Displayed.HasReadyReply() || s.DisplaySerial == serial {
		s.DisplaySerial = serial
		s.Displayed = batch
		return true
	}
	s.PendingSerial = serial
	s.Pending = batch
	s.PendingAvailable = batch.HasReadyReply()
	return true
}

func (s *ReplyState) AcceptPending() bool {
	if !s.PendingAvailable {
		return false
	}
	s.DisplaySerial = s.PendingSerial
	s.Displayed = s.Pending
	s.Pending = ReplyBatch{}
	s.PendingSerial = 0
	s.PendingAvailable = false
	return true
}

// DiscardPartial retains finished choices and removes incomplete output when
// a generation is interrupted or a provider returns an error.
func (s *ReplyState) DiscardPartial() {
	for i := range s.Displayed.Lines {
		if !s.Displayed.Lines[i].Complete {
			s.Displayed.Lines[i] = Line{}
		}
		if !s.Pending.Lines[i].Complete {
			s.Pending.Lines[i] = Line{}
		}
	}
	s.PendingAvailable = s.Pending.HasReadyReply()
}

func (s *ReplyState) ResetWork() {
	s.DiscardPartial()
	s.Next()
}
