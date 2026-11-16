package meeting

const (
	ButtonPause          = 7100
	ButtonCaptions       = 7101
	ButtonSmallerText    = 7102
	ButtonLargerText     = 7103
	ButtonCopyReply      = 7104
	ButtonCopyExpression = 7105
	ButtonRequestReplies = 7106
	ButtonToggleCapture  = 7107
	ButtonOpenProfile    = 7108
	ButtonReplyFirst     = 7109
	ButtonReplySecond    = 7110
	ButtonReplyThird     = 7111
	ButtonAcceptReplies  = 7112
	ButtonRefine         = 7113
	ButtonScrollLatest   = 7114
	ButtonExpression     = 7115
	ButtonSaveProfile    = 7205
	ButtonCloseProfile   = 7206
)

type Command int

const (
	CommandNone Command = iota
	CommandTogglePause
	CommandCaptions
	CommandSmallerText
	CommandLargerText
	CommandCopyReply
	CommandCopyExpression
	CommandRequestReplies
	CommandToggleCapture
	CommandOpenProfile
	CommandSelectReply
	CommandAcceptReplies
	CommandRefine
	CommandScrollLatest
	CommandToggleExpression
	CommandSaveProfile
	CommandCloseProfile
)

// HUDFlags is the meeting view state a button can change.
type HUDFlags struct {
	Paused, CaptionsOnly, ExpressionExpanded, ProfileOpen bool
	SelectedReply                                         int
}

// Effect is the host work that follows a meeting button.
type Effect int

const (
	EffectNone Effect = iota
	EffectRefresh
	EffectLayoutSaveRefresh
	EffectSmallerText
	EffectLargerText
	EffectCopy
	EffectRequestReplies
	EffectToggleCapture
	EffectOpenProfile
	EffectSelectReply
	EffectScrollLatest
	EffectLayoutRefresh
	EffectAccept
	EffectRefine
	EffectSaveProfile
	EffectCloseProfile
)

// NeedsMeetingOwner reports a button that stays disabled until a live meeting owns the window.
func NeedsMeetingOwner(id int) bool {
	switch id {
	case ButtonRequestReplies, ButtonToggleCapture, ButtonOpenProfile:
		return true
	default:
		return false
	}
}

// ApplyCommand returns the view flags after a button and the host effect.
// Captions save the layout. Expanding an expression only lays the window out again.
func ApplyCommand(flags HUDFlags, action Command, replyIndex int) (HUDFlags, Effect) {
	switch action {
	case CommandTogglePause:
		flags.Paused = !flags.Paused
		return flags, EffectRefresh
	case CommandCaptions:
		flags.CaptionsOnly = !flags.CaptionsOnly
		return flags, EffectLayoutSaveRefresh
	case CommandSmallerText:
		return flags, EffectSmallerText
	case CommandLargerText:
		return flags, EffectLargerText
	case CommandCopyReply, CommandCopyExpression:
		return flags, EffectCopy
	case CommandRequestReplies:
		return flags, EffectRequestReplies
	case CommandToggleCapture:
		return flags, EffectToggleCapture
	case CommandOpenProfile:
		return flags, EffectOpenProfile
	case CommandSelectReply:
		flags.SelectedReply = replyIndex
		return flags, EffectSelectReply
	case CommandScrollLatest:
		return flags, EffectScrollLatest
	case CommandToggleExpression:
		flags.ExpressionExpanded = !flags.ExpressionExpanded
		return flags, EffectLayoutRefresh
	case CommandAcceptReplies:
		return flags, EffectAccept
	case CommandRefine:
		return flags, EffectRefine
	case CommandSaveProfile:
		return flags, EffectSaveProfile
	case CommandCloseProfile:
		flags.ProfileOpen = false
		return flags, EffectCloseProfile
	default:
		return flags, EffectNone
	}
}

type PostedMeeting int

const (
	PostedStart PostedMeeting = iota
	PostedSuggest
	PostedDemo
	PostedStop
	PostedRefresh
)

type PostedEffect int

const (
	PostedIgnore PostedEffect = iota
	PostedBegin
	PostedBeginDemo
	PostedReplies
	PostedStopMeeting
	PostedRefreshMeeting
)

// HoldUsesMeeting reports that a live meeting, not a demo, owns the recording that just started.
func HoldUsesMeeting(hasMeeting, demo bool) bool {
	return hasMeeting && !demo
}

// PostedMeetingEffect is a meeting message from the tray. A paused app ignores a real start.
// A demo still starts. Suggestions run only for a live meeting.
func PostedMeetingEffect(message PostedMeeting, paused, hasMeeting, demo bool) PostedEffect {
	switch message {
	case PostedStart:
		if paused {
			return PostedIgnore
		}
		return PostedBegin
	case PostedSuggest:
		if !hasMeeting || demo {
			return PostedIgnore
		}
		return PostedReplies
	case PostedDemo:
		return PostedBeginDemo
	case PostedStop:
		return PostedStopMeeting
	case PostedRefresh:
		return PostedRefreshMeeting
	default:
		return PostedIgnore
	}
}

// CommandFor maps a meeting button id to the action it performs.
// Reply buttons also return the suggestion index, starting at zero.
func CommandFor(id int) (Command, int) {
	switch id {
	case ButtonPause:
		return CommandTogglePause, 0
	case ButtonCaptions:
		return CommandCaptions, 0
	case ButtonSmallerText:
		return CommandSmallerText, 0
	case ButtonLargerText:
		return CommandLargerText, 0
	case ButtonCopyReply:
		return CommandCopyReply, 0
	case ButtonCopyExpression:
		return CommandCopyExpression, 0
	case ButtonRequestReplies:
		return CommandRequestReplies, 0
	case ButtonToggleCapture:
		return CommandToggleCapture, 0
	case ButtonOpenProfile:
		return CommandOpenProfile, 0
	case ButtonReplyFirst, ButtonReplySecond, ButtonReplyThird:
		return CommandSelectReply, id - ButtonReplyFirst
	case ButtonAcceptReplies:
		return CommandAcceptReplies, 0
	case ButtonRefine:
		return CommandRefine, 0
	case ButtonScrollLatest:
		return CommandScrollLatest, 0
	case ButtonExpression:
		return CommandToggleExpression, 0
	case ButtonSaveProfile:
		return CommandSaveProfile, 0
	case ButtonCloseProfile:
		return CommandCloseProfile, 0
	default:
		return CommandNone, 0
	}
}

// CopyText is the clipboard payload for a meeting copy button.
// An unfinished reply copies nothing. Expression copy uses the last finished line.
func CopyText(action Command, replyEnglish string, replyComplete bool, expression string) string {
	if action == CommandCopyExpression {
		return expression
	}
	if !replyComplete {
		return ""
	}
	return replyEnglish
}

func ReplyCopyEnabled(complete bool, english string) bool {
	return complete && english != ""
}

func AcceptEnabled(owner, pending, paused bool) bool {
	return owner && pending && !paused
}

func RefineEnabled(complete bool, instruction string, paused bool) bool {
	return complete && instruction != "" && !paused
}

// RefineButtonEnabled is the button face, not the click. The click still
// requires a typed instruction. The button also requires English and an owner.
func RefineButtonEnabled(owner, complete bool, english string, paused bool) bool {
	return owner && ReplyCopyEnabled(complete, english) && !paused
}

// ButtonPrimary reports whether the owner-draw button uses the primary style.
// Accept and expand follow the live meeting state. Save profile is always primary.
func ButtonPrimary(id int, pending, expressionExpanded bool) bool {
	return id == ButtonRequestReplies ||
		id == ButtonAcceptReplies && pending ||
		id == ButtonExpression && expressionExpanded ||
		id == ButtonSaveProfile
}
