// voicecli is a separate Go module consuming the public VoiceRewrite engine.
// It has no Windows main-package, Swift, cgo, GUI or OS-specific dependency.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/voice"
)

func execute(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("voicecli", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "JSON configuration (defaults when omitted)")
	pcmPath := flags.String("pcm", "", "input 16 kHz mono PCM16 file")
	selectedPath := flags.String("selected-file", "", "selected UTF-8 text for rewrite")
	rewrite := flags.Bool("rewrite", false, "apply spoken instruction to selected text")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *pcmPath == "" || flags.NArg() != 0 {
		return errors.New("usage: voicecli -pcm audio.pcm [-config config.json] [-rewrite -selected-file text.txt]")
	}
	cfg := coreconfig.Defaults()
	if *configPath != "" {
		raw, err := os.ReadFile(*configPath)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		cfg, err = coreconfig.Parse(raw)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}
	var err error
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	pcm, err := os.ReadFile(*pcmPath)
	if err != nil {
		return fmt.Errorf("audio: %w", err)
	}
	var selected string
	if *selectedPath != "" {
		raw, err := os.ReadFile(*selectedPath)
		if err != nil {
			return fmt.Errorf("selection: %w", err)
		}
		selected = string(raw)
	}
	var core voice.Core
	defer core.Apply(voice.Command{Kind: voice.CommandShutdown})
	result, err := core.Execute(core.Apply(voice.Command{Kind: voice.CommandStart}).Token, cfg, pcm, selected, *rewrite, nil)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func main() {
	if err := execute(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
