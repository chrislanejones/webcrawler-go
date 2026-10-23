//go:build unix

package crawler

import (
	"os/exec"
	"syscall"

	"github.com/chromedp/chromedp"
)

// chromeProcessOptions starts Chrome in its own process group. A terminal
// sends Ctrl-C to the whole foreground group, so without this Chrome died
// with the first Ctrl-C and every capture in flight failed, when the point of
// the first Ctrl-C is to let them finish. The crawler still stops Chrome
// itself when each capture's context ends.
func chromeProcessOptions() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
			if cmd.SysProcAttr == nil {
				cmd.SysProcAttr = &syscall.SysProcAttr{}
			}
			cmd.SysProcAttr.Setpgid = true
		}),
	}
}
