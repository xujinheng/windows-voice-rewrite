package main

import coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"

// Config is the portable application configuration model.
type Config = coreconfig.Config

func defaults() Config { return coreconfig.Defaults() }
