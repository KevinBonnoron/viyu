package ytdlp

import (
	"os/exec"
	"strconv"
	"syscall"
)

// killTreeOnCancel makes cancellation end yt-dlp and every process it started (downloader, ffmpeg): Windows
// has no process group to signal, so the tree is ended with taskkill /T. Should taskkill fail, yt-dlp itself
// is still killed.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
