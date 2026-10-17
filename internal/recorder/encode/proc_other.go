//go:build !unix

package encode

import (
	"os"
	"os/exec"
)

func setProcGroup(*exec.Cmd) {}

func terminate(p *os.Process) { _ = p.Kill() }
