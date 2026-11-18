//go:build !windows

package main

func acquireLaunchLock() (func(), error) { return func() {}, nil }
