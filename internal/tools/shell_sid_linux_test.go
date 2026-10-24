//go:build linux

package tools

import (
	"os"
	"strconv"
	"strings"
)

// sessionID is this process's session id, the sixth field of /proc/self/stat (after the parenthesized command,
// which may itself hold spaces).
func sessionID() int {
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	rest := string(stat)
	if i := strings.LastIndex(rest, ")"); i >= 0 {
		rest = rest[i+1:]
	}
	fields := strings.Fields(rest) // state, ppid, pgrp, session, ...
	if len(fields) < 4 {
		return 0
	}
	sid, _ := strconv.Atoi(fields[3])
	return sid
}
