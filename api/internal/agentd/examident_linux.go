//go:build linux

package agentd

import (
	"fmt"
	"os"
	"strings"
)

// procIdentity is process pid's identity, which no other process has while
// this boot lasts: the boot's id and when the process started, in clock ticks
// after boot (/proc/<pid>/stat, field 22; review 419 B2: ps's start time is
// to the second, which two processes can share).
func procIdentity(pid int) (string, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// The command name (field 2) is in parentheses and may hold spaces: the
	// fields after it start past its last ')'.
	i := strings.LastIndexByte(string(stat), ')')
	fields := strings.Fields(string(stat[i+1:]))
	if i < 0 || len(fields) < 20 {
		return "", fmt.Errorf("agentd: /proc/%d/stat does not read", pid)
	}
	return "linux:" + strings.TrimSpace(string(boot)) + "/" + fields[19], nil
}
