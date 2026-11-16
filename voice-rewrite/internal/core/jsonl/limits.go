// Package jsonl holds the voicebridge frame limits shared with the macOS client.
package jsonl

const (
	Version         = 1
	MaxRequestBytes = 8 << 20
	MaxIDBytes      = 128
	MaxSeenIDs      = 65536
)
