package main

import coreconfig "voicerewrite/internal/core/config"

// Config is the portable application configuration model.
type Config = coreconfig.Config

func defaults() Config { return coreconfig.Defaults() }
