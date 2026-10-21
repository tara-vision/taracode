package evals

import "os"

// setCallEnv sets a record call's env in the process for the call's execution and returns what restores it: the
// tools run commands with the process environment, and the recorder runs one call at a time.
func setCallEnv(env map[string]string) (restore func()) {
	if len(env) == 0 {
		return func() {}
	}
	previous := map[string]*string{}
	for k, v := range env {
		if old, ok := os.LookupEnv(k); ok {
			previous[k] = &old
		} else {
			previous[k] = nil
		}
		_ = os.Setenv(k, v)
	}
	return func() {
		for k, old := range previous {
			if old == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *old)
			}
		}
	}
}
