package meeting

import (
	"context"
	"errors"

	"voicerewrite/internal/core/audio"
)

// CaptureEvent is the portable source/task intent. The native host owns only
// enumerating devices, collecting PCM and executing the requested transition.
type CaptureEvent string

const (
	CaptureStart  CaptureEvent = "start"
	CapturePause  CaptureEvent = "pause"
	CaptureResume CaptureEvent = "resume"
	CaptureSwitch CaptureEvent = "switch"
	CaptureLost   CaptureEvent = "source_lost"
	CaptureStop   CaptureEvent = "stop"
)

var (
	ErrCaptureSource  = errors.New("meeting source is unavailable")
	ErrCaptureActive  = errors.New("meeting capture has already started")
	ErrCaptureEpoch   = errors.New("meeting capture epoch is stale")
	ErrCaptureStopped = errors.New("meeting capture is stopped")
)

// CaptureRoute applies source and pause transitions to the EXISTING session
// runtime; it does not create another session, worker or callback queue.
// expectedEpoch must match for asynchronous source changes/loss notifications.
func (s *SessionRuntime) CaptureRoute(event CaptureEvent, source string, expectedEpoch uint64) (uint64, error) {
	return CaptureRoute(s, event, source, expectedEpoch)
}

// CaptureRoute is the shared Go entrypoint called by the Windows host and
// macOS voicebridge, with identical epoch, source and cancellation rules.
func CaptureRoute(s *SessionRuntime, event CaptureEvent, source string, expectedEpoch uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return s.epoch, ErrCaptureStopped
	}
	if expectedEpoch != 0 && expectedEpoch != s.epoch {
		return s.epoch, ErrCaptureEpoch
	}
	switch event {
	case CaptureStart:
		if source == "" {
			return s.epoch, ErrCaptureSource
		}
		if s.sourceID != "" {
			return s.epoch, ErrCaptureActive
		}
		s.sourceID = source
	case CaptureSwitch:
		if source == "" {
			return s.epoch, ErrCaptureSource
		}
		if s.sourceID == "" {
			return s.epoch, ErrCaptureSource
		}
		if source != s.sourceID {
			s.sourceID = source
			s.resetLocked()
		}
	case CapturePause:
		if !s.paused {
			s.paused = true
			s.resetLocked()
		}
	case CaptureResume:
		if s.sourceLost {
			return s.epoch, ErrCaptureSource
		}
		if s.paused {
			s.paused = false
			s.resetLocked()
		}
	case CaptureLost:
		// A failed native source fences all queued ASR, captions and replies.
		// Recovery requires a deliberate new source, not implicit resume.
		s.sourceLost = true
		if !s.paused {
			s.paused = true
			s.resetLocked()
		}
	case CaptureStop:
		s.stopLocked()
	default:
		return s.epoch, errors.New("unknown meeting capture event")
	}
	if event == CaptureSwitch || event == CaptureStart {
		s.sourceLost = false
	}
	return s.epoch, nil
}

// CaptureSource returns the Go-owned source binding. It is for observation;
// device identity and operating-system permissions remain native.
func (s *SessionRuntime) CaptureSource() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sourceID
}

// QueueKind distinguishes packets at the OS bridge from shared ASR work.
type QueueKind uint8

const (
	QueuePacket QueueKind = iota
	QueueFinal
	QueuePreview
	QueueCaption
)

type QueueAction uint8

const (
	QueueAppend QueueAction = iota
	QueueReplaceOldest
	QueueDropIncoming
)

// PacketWindow limits *in-flight bridge requests* separately from the 12
// buffered 20ms-aligned packet batches in the Go audio adapter. Only Go owns
// these limits; Swift implements the advertised transport credit window.
const (
	PacketWindow  = 8
	PacketBacklog = 12
)

// DecideQueue is shared by the macOS bridge and the Windows/macOS Pipeline.
// Final ASR and caption revisions coalesce the oldest queued work; native
// ingress drops a new packet instead of blocking the 20ms capture thread.
func DecideQueue(kind QueueKind, queued, capacity int) QueueAction {
	if capacity <= 0 {
		return QueueDropIncoming
	}
	if queued < capacity {
		return QueueAppend
	}
	if kind == QueuePacket {
		return QueueDropIncoming
	}
	return QueueReplaceOldest
}

// AdmitPCM checks epoch and packet shape before queue admission, including
// after pause, source loss, configuration reset and stop. It is intentionally
// nonblocking, so a native callback never waits for ASR or translation.
func (s *SessionRuntime) AdmitPCM(epoch uint64, size, queued, capacity int) (QueueAction, error) {
	if size <= 0 || size > 32000 || size%audio.FrameBytes != 0 {
		return QueueDropIncoming, errors.New("meeting PCM is not 20ms aligned")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return QueueDropIncoming, context.Canceled
	}
	if s.paused || s.work.Err() != nil || s.sourceLost {
		return QueueDropIncoming, errors.New("meeting capture paused")
	}
	if epoch != s.epoch {
		return QueueDropIncoming, ErrCaptureEpoch
	}
	return DecideQueue(QueuePacket, queued, capacity), nil
}
