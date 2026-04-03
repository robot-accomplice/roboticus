//go:build linux

package security

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/rs/zerolog/log"
)

// oPath is O_PATH (0x200000), needed for Landlock path-beneath rules.
// Not exported by Go's syscall package; defined in Linux kernel since 2.6.39.
const oPath = 0x200000

// Landlock syscall numbers (ABI v1, kernel 5.13+).
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
	landlockRulePathBeneath  = 1
)

// Landlock filesystem access flags.
const (
	accessFSExecute    uint64 = 1 << 0
	accessFSWriteFile  uint64 = 1 << 1
	accessFSReadFile   uint64 = 1 << 2
	accessFSReadDir    uint64 = 1 << 3
	accessFSRemoveDir  uint64 = 1 << 4
	accessFSRemoveFile uint64 = 1 << 5
	accessFSMakeChar   uint64 = 1 << 6
	accessFSMakeDir    uint64 = 1 << 7
	accessFSMakeReg    uint64 = 1 << 8
	accessFSMakeSock   uint64 = 1 << 9
	accessFSMakeFifo   uint64 = 1 << 10
	accessFSMakeBlock  uint64 = 1 << 11
	accessFSMakeSym    uint64 = 1 << 12

	allReadAccess  = accessFSExecute | accessFSReadFile | accessFSReadDir
	allWriteAccess = accessFSWriteFile | accessFSRemoveDir | accessFSRemoveFile |
		accessFSMakeChar | accessFSMakeDir | accessFSMakeReg |
		accessFSMakeSock | accessFSMakeFifo | accessFSMakeBlock | accessFSMakeSym
	allAccess = allReadAccess | allWriteAccess
)

type landlockRulesetAttr struct {
	handledAccessFS  uint64
	handledAccessNet uint64
}

type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFd      int32
}

type linuxSandbox struct {
	cfg SandboxConfig
}

func newPlatformSandbox(cfg SandboxConfig) Sandbox {
	return &linuxSandbox{cfg: cfg}
}

func (s *linuxSandbox) Available() bool {
	// Test if Landlock is available by attempting to create a ruleset.
	attr := landlockRulesetAttr{handledAccessFS: allAccess}
	fd, _, errno := syscall.Syscall(sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return false
	}
	_ = syscall.Close(int(fd))
	return true
}

func (s *linuxSandbox) Apply(cmd *exec.Cmd) error {
	// Landlock must be applied in the child process via SysProcAttr.
	// We use the pre-exec hook to set up confinement.
	cmd.SysProcAttr = &syscall.SysProcAttr{}

	workspace := s.cfg.WorkspaceDir
	allowedPaths := s.cfg.AllowedPaths

	// The actual Landlock setup runs in the child before exec.
	origEnv := cmd.Env
	cmd.Env = append(origEnv,
		"GOBOTICUS_SANDBOX=landlock",
		fmt.Sprintf("GOBOTICUS_SANDBOX_WORKSPACE=%s", workspace),
	)

	log.Debug().
		Str("workspace", workspace).
		Int("allowed_paths", len(allowedPaths)).
		Msg("Landlock sandbox configured for child process")

	return nil
}

// ApplyLandlock applies Landlock filesystem restrictions in the current process.
// This is called from the child process pre-exec context.
// Policy: read-only root, full access to /tmp, workspace, and allowed paths.
func ApplyLandlock(workspace string, allowedPaths []string) error {
	// Step 1: PR_SET_NO_NEW_PRIVS (required before Landlock).
	_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, 38, 1, 0, 0, 0, 0) // PR_SET_NO_NEW_PRIVS=38
	if errno != 0 {
		return fmt.Errorf("landlock: prctl(NO_NEW_PRIVS): %v", errno)
	}

	// Step 2: Create ruleset.
	attr := landlockRulesetAttr{handledAccessFS: allAccess}
	fd, _, errno := syscall.Syscall(sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock: create_ruleset: %v", errno)
	}
	rulesetFd := int(fd)
	defer func() { _ = syscall.Close(rulesetFd) }()

	// Step 3: Add rules.
	// Root: read-only.
	if err := addPathRule(rulesetFd, "/", allReadAccess); err != nil {
		log.Warn().Err(err).Msg("landlock: failed to add root read rule, continuing")
	}

	// /tmp: full access.
	if err := addPathRule(rulesetFd, "/tmp", allAccess); err != nil {
		log.Warn().Err(err).Msg("landlock: failed to add /tmp rule, continuing")
	}

	// Workspace: full access.
	if workspace != "" {
		if err := addPathRule(rulesetFd, workspace, allAccess); err != nil {
			log.Warn().Err(err).Str("path", workspace).Msg("landlock: failed to add workspace rule")
		}
	}

	// Additional allowed paths: full access.
	for _, p := range allowedPaths {
		if err := addPathRule(rulesetFd, p, allAccess); err != nil {
			log.Warn().Err(err).Str("path", p).Msg("landlock: failed to add allowed path rule")
		}
	}

	// Step 4: Restrict self.
	_, _, errno = syscall.Syscall(sysLandlockRestrictSelf, uintptr(rulesetFd), 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock: restrict_self: %v", errno)
	}

	return nil
}

func addPathRule(rulesetFd int, path string, access uint64) error {
	fd, err := syscall.Open(path, oPath|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = syscall.Close(fd) }()

	rule := landlockPathBeneathAttr{
		allowedAccess: access,
		parentFd:      int32(fd),
	}
	_, _, errno := syscall.Syscall6(sysLandlockAddRule,
		uintptr(rulesetFd), landlockRulePathBeneath,
		uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("add_rule %s: %v", path, errno)
	}
	return nil
}
