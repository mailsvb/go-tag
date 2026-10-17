//go:build unix

package encode

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcGroup puts ffmpeg into its own process group, so a terminal Ctrl-C
// reaches go-tag only and ffmpeg can finish the file when stdin closes.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(p *os.Process) { _ = p.Signal(syscall.SIGTERM) }
