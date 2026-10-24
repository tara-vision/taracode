//go:build darwin

package tools

import "syscall"

// sessionID is this process's session id.
func sessionID() int {
	sid, err := syscall.Getsid(0)
	if err != nil {
		return 0
	}
	return sid
}
