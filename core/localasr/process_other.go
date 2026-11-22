//go:build !windows

package localasr

// macOS/Linux use normal application shutdown to stop their managed child.
// Windows additionally requires a Job Object to kill children if the UI crashes.
func protectChild(_ int) (func(), error) { return nil, nil }
