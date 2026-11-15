package provider

import "regexp"

// ThinkingTag removes complete reasoning spans before rendering model output.
var ThinkingTag = regexp.MustCompile(`(?s)<think>.*?</think>\\s*`)
