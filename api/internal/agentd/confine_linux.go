//go:build linux

package agentd

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Landlock (Linux 5.13+), through the system calls themselves: the standard
// library has no wrapper and the numbers are the same on every architecture.
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446

	landlockCreateRulesetVersion = 1
	landlockRulePathBeneath      = 1
	prSetNoNewPrivs              = 38
	oPath                        = 0x200000

	llWriteFile  = 1 << 1
	llRemoveDir  = 1 << 4
	llRemoveFile = 1 << 5
	llMakeChar   = 1 << 6
	llMakeDir    = 1 << 7
	llMakeReg    = 1 << 8
	llMakeSock   = 1 << 9
	llMakeFifo   = 1 << 10
	llMakeBlock  = 1 << 11
	llMakeSym    = 1 << 12
	llRefer      = 1 << 13 // ABI 2
	llTruncate   = 1 << 14 // ABI 3
)

// wallABI is the least Landlock ABI the wall takes: ABI 3 is the first that
// governs truncate(2) and O_TRUNC, without which a file outside the wall
// could be emptied.
const wallABI = 3

// wallWrites are every right that changes the file system; reading and
// executing are not governed (the same user may read: v5 §6.2).
const wallWrites = llWriteFile | llRemoveDir | llRemoveFile | llMakeChar | llMakeDir | llMakeReg | llMakeSock | llMakeFifo |
	llMakeBlock | llMakeSym | llRefer | llTruncate

// wallDevices may be written (and truncated: a shell's "> /dev/null" opens
// with O_TRUNC) by everything in the wall.
var wallDevices = []string{"/dev/null", "/dev/zero", "/dev/full", "/dev/tty", "/dev/ptmx", "/dev/pts"}

// landlockABI is the kernel's Landlock ABI version.
func landlockABI() (int, error) {
	r, _, e := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if e != 0 {
		return 0, fmt.Errorf("agentd: no Landlock on this kernel: %w", e)
	}
	return int(r), nil
}

// restrictWrites holds the calling thread, and whatever it executes from now
// on, to writing beneath dirs and the devices: the caller has locked the
// goroutine to its thread and execs on it. Each of dirs must be a directory,
// not a link; nothing is restricted when any step fails, and the caller must
// then not run what it was to confine.
func restrictWrites(dirs []string) error {
	abi, err := landlockABI()
	if err != nil {
		return err
	}
	if abi < wallABI {
		return fmt.Errorf("agentd: Landlock ABI %d on this kernel; the wall needs %d (truncate)", abi, wallABI)
	}
	attr := struct{ fs uint64 }{wallWrites}
	r, _, e := syscall.Syscall(sysLandlockCreateRuleset, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if e != 0 {
		return fmt.Errorf("agentd: landlock_create_ruleset: %w", e)
	}
	ruleset := int(r)
	defer func() { _ = syscall.Close(ruleset) }()
	for _, d := range dirs {
		if err = addRule(ruleset, d, syscall.O_DIRECTORY|syscall.O_NOFOLLOW, wallWrites); err != nil {
			return err
		}
	}
	for _, dev := range wallDevices {
		if _, serr := os.Stat(dev); errors.Is(serr, os.ErrNotExist) {
			continue
		}
		if err = addRule(ruleset, dev, 0, llWriteFile|llTruncate); err != nil {
			return err
		}
	}
	if _, _, e = syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); e != 0 {
		return fmt.Errorf("agentd: no_new_privs: %w", e)
	}
	if _, _, e = syscall.Syscall(sysLandlockRestrictSelf, uintptr(ruleset), 0, 0); e != 0 {
		return fmt.Errorf("agentd: landlock_restrict_self: %w", e)
	}
	return nil
}

// addRule lets the ruleset's domain have access beneath path.
func addRule(ruleset int, path string, flags int, access uint64) error {
	fd, err := syscall.Open(path, oPath|syscall.O_CLOEXEC|flags, 0)
	if err != nil {
		return fmt.Errorf("agentd: the wall's %s: %w", path, err)
	}
	defer func() { _ = syscall.Close(fd) }()
	// struct landlock_path_beneath_attr is packed: 8 bytes of access, then
	// the descriptor; the kernel reads those 12 bytes.
	attr := struct {
		access uint64
		parent int32
	}{access, int32(fd)}
	if _, _, e := syscall.Syscall6(sysLandlockAddRule, uintptr(ruleset), landlockRulePathBeneath, uintptr(unsafe.Pointer(&attr)), 0, 0, 0); e != 0 {
		return fmt.Errorf("agentd: the wall's rule for %s: %w", path, e)
	}
	return nil
}
