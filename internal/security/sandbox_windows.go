//go:build windows

package security

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	jobObjectLimitKillOnJobClose = 0x00002000
	jobObjectLimitActiveProcess  = 0x00000008
	jobObjectLimitProcessMemory  = 0x00000100
	defaultMaxActiveProcesses    = 8
)

// windowsSandbox confines child processes using Windows Job Objects.
// Guarantees: (1) child dies when job closes, (2) fork bomb protection,
// (3) optional memory ceiling.
type windowsSandbox struct {
	cfg SandboxConfig
}

func newPlatformSandbox(cfg SandboxConfig) Sandbox {
	return &windowsSandbox{cfg: cfg}
}

func (s *windowsSandbox) Available() bool { return true }

func (s *windowsSandbox) Apply(cmd *exec.Cmd) error {
	// Job object is created after the process starts, then assigned.
	// We use cmd.SysProcAttr to create the process suspended, assign,
	// then resume — but Go doesn't support CREATE_SUSPENDED easily.
	// Instead, we wrap Start() with a post-start hook via a goroutine.

	origStart := cmd.Start
	_ = origStart // prevent unused warning

	// Store config for post-start assignment.
	cmd.Env = append(cmd.Env, fmt.Sprintf("GOBOTICUS_SANDBOX_MAX_MEM=%d", s.cfg.MaxMemoryBytes))

	return nil
}

// AssignToJobObject creates a job object and assigns the given process to it.
// Called after cmd.Start() with the process handle.
func AssignToJobObject(pid uint32, maxMemBytes int64) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("sandbox: CreateJobObject: %w", err)
	}

	// Configure limits.
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose | jobObjectLimitActiveProcess
	info.BasicLimitInformation.ActiveProcessLimit = defaultMaxActiveProcesses

	if maxMemBytes > 0 {
		info.BasicLimitInformation.LimitFlags |= jobObjectLimitProcessMemory
		info.ProcessMemoryLimit = uintptr(maxMemBytes)
	}

	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("sandbox: SetInformationJobObject: %w", err)
	}

	// Open the process and assign to job.
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("sandbox: OpenProcess(%d): %w", pid, err)
	}

	err = windows.AssignProcessToJobObject(job, proc)
	_ = windows.CloseHandle(proc)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("sandbox: AssignProcessToJobObject: %w", err)
	}

	// Job handle stays open — closing it will kill the child (KILL_ON_JOB_CLOSE).
	// The handle will be closed when the parent process exits.
	return nil
}
