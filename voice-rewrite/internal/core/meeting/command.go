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

func RefineEnabled(complete bool, instruction string, paused bool) bool {
	return complete && instruction != "" && !paused
}

// ButtonPrimary reports whether the owner-draw button uses the primary style.
// Accept and expand follow the live meeting state. Save profile is always primary.
func ButtonPrimary(id int, pending, expressionExpanded bool) bool {
	return id == ButtonRequestReplies ||
		id == ButtonAcceptReplies && pending ||
		id == ButtonExpression && expressionExpanded ||
		id == ButtonSaveProfile
}
