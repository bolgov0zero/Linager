//go:build linux

package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// errNoGst is what the helper reports when GStreamer is not installed on the computer.
var errNoGst = errors.New("gst-launch-1.0 is not installed: the Assistant reads the screen of GNOME with GStreamer (apt install gstreamer1.0-tools gstreamer1.0-pipewire)")

// gstCapture reads the frames of a PipeWire stream with `gst-launch-1.0 pipewiresrc` and keeps the newest one. The
// pipeline scales the picture to the size asked for (the size of the monitor, so that nothing is scaled in practice) and
// writes raw BGRA frames to a pipe of ours (the file descriptor 3 of the child), so the size of a frame is known.
type gstCapture struct {
	cmd  *exec.Cmd
	w, h int

	mu    sync.Mutex
	frame []byte // the newest frame
	n     uint64 // the number of frames taken
	err   error

	first chan struct{} // closed when the first frame is in
	done  chan struct{} // closed when the pipeline has ended
}

// startGst starts the pipeline for the PipeWire node; w and h are the size of the frames. fps limits the rate of the
// frames (0: no limit). notify is called after every frame that came.
func startGst(node uint32, fps, w, h int, notify func()) (*gstCapture, error) {
	path, err := exec.LookPath("gst-launch-1.0")
	if err != nil {
		return nil, errNoGst
	}
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 {
		return nil, fmt.Errorf("the size of the screen is not known (%dx%d)", w, h)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	args := []string{"-q", "pipewiresrc", "path=" + strconv.FormatUint(uint64(node), 10), "always-copy=true", "do-timestamp=true", "!"}
	if fps > 0 {
		args = append(args, "videorate", "drop-only=true", "max-rate="+strconv.Itoa(fps), "!")
	}
	args = append(args, "videoconvert", "!", "videoscale", "!", fmt.Sprintf("video/x-raw,format=BGRA,width=%d,height=%d", w, h), "!", "fdsink", "fd=3", "sync=false")
	cmd := exec.Command(path, args...) //nolint:gosec // the path of gst-launch-1.0 and numbers
	cmd.ExtraFiles = []*os.File{pw}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	_ = pw.Close() // the child holds its own copy
	c := &gstCapture{cmd: cmd, w: w, h: h, first: make(chan struct{}), done: make(chan struct{})}
	go func() { // the frames
		defer func() { _ = pr.Close() }()
		buf := make([]byte, w*h*4)
		for {
			if _, err := io.ReadFull(pr, buf); err != nil {
				c.fail(fmt.Errorf("the stream of the screen ended: %w", err))
				return
			}
			c.mu.Lock()
			if len(c.frame) != len(buf) {
				c.frame = make([]byte, len(buf))
			}
			copy(c.frame, buf)
			c.n++
			first := c.n == 1
			c.mu.Unlock()
			if first {
				close(c.first)
			}
			if notify != nil {
				notify()
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		c.fail(fmt.Errorf("gst-launch-1.0 ended: %w", err))
	}()
	return c, nil
}

// fail records why the capture ended (the first reason stays) and stops the pipeline.
func (c *gstCapture) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
	c.mu.Unlock()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// stop ends the pipeline.
func (c *gstCapture) stop() { c.fail(errors.New("stopped")) }

// Err tells why the capture ended.
func (c *gstCapture) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// copyFrame copies the newest frame into dst (grown when needed).
func (c *gstCapture) copyFrame(dst []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(dst) != len(c.frame) {
		dst = make([]byte, len(c.frame))
	}
	copy(dst, c.frame)
	return dst
}

// waitFirst waits for the first frame.
func (c *gstCapture) waitFirst(d time.Duration) error {
	select {
	case <-c.first:
		return nil
	case <-c.done:
		return c.Err()
	case <-time.After(d):
		return errNoPicture
	}
}

// errNoPicture: nothing came from the screen in time (a screen that does not change sends nothing).
var errNoPicture = errors.New("no picture came from the screen in time")
