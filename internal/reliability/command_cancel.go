package reliability

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// BindCommandCancellation isolates a CommandContext child in its own process
// group, kills that group on cancellation and bounds inherited-pipe cleanup.
// It must run before Start. Children that create separate sessions remain an
// operator/process-supervisor concern, outside this group ownership contract.
func BindCommandCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 100 * time.Millisecond
}
