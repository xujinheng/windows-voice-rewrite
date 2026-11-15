// voicebridge is the local JSON-lines process entry point for the macOS host.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const protocolVersion = 1
const maxRequestBytes = 8 << 20

type request struct {
	Version int    `json:"v"`
	ID      string `json:"id"`
	Op      string `json:"op"`
}
type bridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type response struct {
	Version int          `json:"v"`
	ID      string       `json:"id"`
	OK      bool         `json:"ok"`
	Result  any          `json:"result,omitempty"`
	Error   *bridgeError `json:"error,omitempty"`
}

func serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if err := encoder.Encode(response{Version: protocolVersion, OK: false,
				Error: &bridgeError{Code: "invalid_json", Message: "invalid request JSON"}}); err != nil {
				return err
			}
			continue
		}
		reply := response{Version: protocolVersion, ID: req.ID}
		switch {
		case req.Version != protocolVersion:
			reply.Error = &bridgeError{Code: "protocol_version", Message: "unsupported protocol version"}
		case req.ID == "" || len(req.ID) > 128:
			reply.Error = &bridgeError{Code: "invalid_id", Message: "request ID must be 1..128 bytes"}
		case req.Op == "hello":
			reply.OK = true
			reply.Result = map[string]any{"name": "VoiceRewrite", "protocol": protocolVersion}
		default:
			reply.Error = &bridgeError{Code: "unsupported_op", Message: "unsupported operation"}
		}
		if err := encoder.Encode(reply); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		// Never echo request bytes, credentials or private text to stderr.
		if !errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, "voicebridge: input/output failure")
		}
		os.Exit(1)
	}
}
