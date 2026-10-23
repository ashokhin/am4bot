//go:build linux

package bot

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
)

// headlessViaXvfb starts a real X virtual frame buffer and points Chrome's
// DISPLAY/XAUTHORITY at it, instead of passing Chrome's own --headless
// flag - see newChromeContext's doc comment for why. Xvfb is Linux-only.
func headlessViaXvfb() ([]chromedp.ExecAllocatorOption, func(), error) {
	fb, err := newXvfbFrameBuffer("1920x1080x24")
	if err != nil {
		return nil, func() {}, err
	}

	opt := chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
		cmd.Env = append(cmd.Env, "DISPLAY=:"+fb.display, "XAUTHORITY="+fb.authPath)

		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = new(syscall.SysProcAttr)
		}

		// When the parent (this Go process) dies, kill Chrome too, same as
		// the Xvfb process itself (see newXvfbFrameBuffer).
		cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
	})

	return []chromedp.ExecAllocatorOption{opt}, fb.stop, nil
}

// xvfbFrameBuffer controls an X virtual frame buffer running as a
// background process - see newXvfbFrameBuffer.
type xvfbFrameBuffer struct {
	display  string
	authPath string
	cmd      *exec.Cmd
}

// newXvfbFrameBuffer starts Xvfb in the background and waits for it to
// report which X11 display it's listening on. screenSize is Xvfb's own
// "-screen 0 <WxHxD>" argument.
func newXvfbFrameBuffer(screenSize string) (*xvfbFrameBuffer, error) {
	if err := exec.Command("which", "Xvfb").Run(); err != nil {
		return nil, fmt.Errorf("xvfb not found (chrome_stealth with chrome_headless requires it to be installed): %w", err)
	}

	authFile, err := os.CreateTemp("", "chromedp-xvfb")
	if err != nil {
		return nil, err
	}
	authPath := authFile.Name()
	if err := authFile.Close(); err != nil {
		return nil, err
	}

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer pipeReader.Close()

	// Xvfb prints the display number it picked to fd 3, which we hand it
	// as the write end of this pipe - the only way to learn which display
	// it chose when we don't pick one ourselves (":0" is normally taken by
	// the host's own X server).
	xvfb := exec.Command("Xvfb", "-displayfd", "3", "-nolisten", "tcp", "-screen", "0", screenSize)
	xvfb.ExtraFiles = []*os.File{pipeWriter}
	xvfb.Env = append(os.Environ(), "XAUTHORITY="+authPath)
	xvfb.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}

	if err := xvfb.Start(); err != nil {
		return nil, err
	}
	pipeWriter.Close()

	display, err := readXvfbDisplayNumber(pipeReader)
	if err != nil {
		_ = xvfb.Process.Kill()

		return nil, err
	}

	xauth := exec.Command("xauth", "generate", ":"+display, ".", "trusted")
	xauth.Env = append(os.Environ(), "XAUTHORITY="+authPath)
	if err := xauth.Run(); err != nil {
		_ = xvfb.Process.Kill()

		return nil, fmt.Errorf("xauth generate: %w", err)
	}

	return &xvfbFrameBuffer{display: display, authPath: authPath, cmd: xvfb}, nil
}

func readXvfbDisplayNumber(pipeReader *os.File) (string, error) {
	type result struct {
		display string
		err     error
	}

	ch := make(chan result, 1)

	go func() {
		buf := make([]byte, 32)
		n, err := pipeReader.Read(buf)
		ch <- result{strings.TrimSpace(string(buf[:n])), err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}

		return res.display, nil
	case <-time.After(10 * time.Second):
		return "", fmt.Errorf("timeout waiting for Xvfb to report its display number")
	}
}

// stop kills the Xvfb process and removes its X11 authorization file.
func (fb *xvfbFrameBuffer) stop() {
	if fb == nil || fb.cmd == nil || fb.cmd.Process == nil {
		return
	}

	_ = fb.cmd.Process.Kill()
	_ = fb.cmd.Wait()
	_ = os.Remove(fb.authPath)
}
