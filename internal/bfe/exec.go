package bfe

import (
	"os/exec"
	"syscall"

	"github.com/mitchellh/go-ps"
	"k8s.io/klog"
)

const (
	defBfeBinary  = "/usr/local/bin/bfe/bfe"
	defBfeCfgPath = "/etc/bfe/bfe/conf"
)

//Exec defines the interface to execute
//command like reload or test configuration
type Exec interface {
	ExecCommand(args ...string) *exec.Cmd
	Test(cfg string) ([]string, error)
}

//Command store context around a given bfe executable path
type Command struct {
	Binary string
	Cmd    *exec.Cmd
}

//NewCommand return a new Command from given bfe binary path
func NewCommand() *Command {
	return &Command{
		Binary: defBfeBinary,
	}
}

//ExecCommand instanciates an exec.Cmd object to all nginx program
func (bc *Command) ExecCommand(args ...string) *exec.Cmd {
	cmdArgs := []string{}
	cmdArgs = append(cmdArgs, "-c", defBfeCfgPath)
	cmdArgs = append(cmdArgs, args...)
	bc.Cmd = exec.Command(bc.Binary, cmdArgs...)
	return bc.Cmd
}

//IsRespawnIfRequired check error type is exec.ExitError or not
func IsRespawnIfRequired(err error) bool {
	exitError, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	waitStatus := exitError.Sys().(syscall.WaitStatus)
	klog.Warningf("bfe process died (%v):%v", waitStatus.ExitStatus(), err)
	return true
}

//IsRunning check bfe process exit.if exit and err not nil
func IsRunning(pid int) bool {
	ps, err := ps.FindProcess(pid)
	if err != nil {
		klog.Warning("find bfe process error %w", err)
		return false
	}
	if ps != nil {
		return false
	}
	return true
}
