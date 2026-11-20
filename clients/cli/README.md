# VoiceRewrite CLI (independent Go client)

This is a **separate Go module** that imports the public `config` and `voice`
packages. It does not import Windows `main`, any `internal` package, Swift,
AppKit or a C archive. Its `replace` directive points to the local Core for
development; a tagged Core release can replace that directive later.

From `clients/cli/`:

```sh
go run . -pcm recorded-16khz-mono-s16le.pcm -config config.json
go run . -pcm instruction-16khz-mono-s16le.pcm -config config.json -rewrite -selected-file selected.txt
```

Without `-config`, Core defaults are used. The input is **raw** little-endian
16 kHz mono signed 16-bit PCM, not WAV/MP3. Output is the *same* JSON
`voice.Result` returned to desktop clients: `text`, `intent`,
`instruction`, `route`. The CLI deliberately does not paste into a desktop
editor; it writes the result to stdout. Provider/network errors go to stderr
and set a nonzero exit status.

```sh
go test ./...
go build .
```
