//go:build unix

package ytdlp

import (
	"os/exec"
	"syscall"
)

// killTreeOnCancel starts yt-dlp as the leader of its own process group and makes cancellation kill the
// whole group, its downloader and ffmpeg children included.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
