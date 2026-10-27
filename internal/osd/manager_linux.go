//go:build linux

package osd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Config selects a directly executed program, never a shell command.
// Report must return promptly and must not call back into the Manager.
type Config struct {
	Executable string
	Report     func(error)
}

type Manager struct {
	mu         sync.Mutex
	executable string
	report     func(error)
	current    *instance
	closed     bool
}

type instance struct {
	cmd        *exec.Cmd
	stdin      *os.File
	done       chan struct{}
	stopping   atomic.Bool
	cleanupErr error // read only after done closes
}

type boundedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= 4096 {
		b.data = append(b.data[:0], p[n-4096:]...)
	} else {
		if extra := len(b.data) + n - 4096; extra > 0 {
			b.data = b.data[extra:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *boundedOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }

func arguments() []string {
	return []string{"--position", "0", "--x-offset", "32", "--y-offset", "32", "--padding", "16", "--transparency", "0", "--font", "monospace 24", "--back-color", "black", "--fore-color", "white", "--fade-in", "0", "--fade-full", "5000", "--fade-out", "0", "--lines", "1"}
}

func New(config Config) (*Manager, error) {
	executable := config.Executable
	if executable == "" {
		executable = "aosd_cat"
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("find OSD executable: %w", err)
	}
	report := config.Report
	if report == nil {
		report = func(error) {}
	}
	return &Manager{executable: resolved, report: report}, nil
}

func signalGroup(c *instance, signal syscall.Signal) error {
	// Start creates this group with PGID equal to our direct child's PID.
	// Never discover or signal an inherited/shared process group.
	err := syscall.Kill(-c.cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func waitFor(done <-chan struct{}, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (m *Manager) stopLocked() error {
	c := m.current
	if c == nil {
		return nil
	}
	finish := func() error {
		if c.cleanupErr != nil {
			return c.cleanupErr
		}
		m.current = nil
		return nil
	}
	select {
	case <-c.done:
		return finish()
	default:
	}
	c.stopping.Store(true)
	_ = c.stdin.Close()
	for _, delay := range []time.Duration{20 * time.Millisecond, 40 * time.Millisecond} {
		if err := signalGroup(c, syscall.SIGTERM); err != nil {
			return fmt.Errorf("terminate OSD group: %w", err)
		}
		if waitFor(c.done, delay) {
			return finish()
		}
	}
	if err := signalGroup(c, syscall.SIGKILL); err != nil {
		return fmt.Errorf("kill OSD group: %w", err)
	}
	if !waitFor(c.done, time.Second) {
		return errors.New("OSD child did not finish cleanup after SIGKILL")
	}
	return finish()
}

// Show replaces previous text without waiting for its five-second display.
// An empty message clears. The stdin pipe stays open until exit or replacement.
func (m *Manager) Show(message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("OSD manager closed")
	}
	if len(message) > 4095 || strings.ContainsAny(message, "\r\n") {
		return errors.New("OSD text must be one line of at most 4095 bytes")
	}
	if err := m.stopLocked(); err != nil {
		return err
	}
	if message == "" {
		return nil
	}
	cmd := exec.Command(m.executable, arguments()...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 250 * time.Millisecond
	output := &boundedOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdin, ok := pipe.(*os.File)
	if !ok {
		pipe.Close()
		return errors.New("OSD stdin does not support bounded writes")
	}
	if err := stdin.SetWriteDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
		stdin.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("start OSD: %w", err)
	}
	c := &instance{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	m.current = c
	go func() {
		waitErr := cmd.Wait()
		// Also retire descendants if the direct child exits on its own. Completed
		// instances are never signalled again after done has closed.
		c.cleanupErr = signalGroup(c, syscall.SIGKILL)
		_ = stdin.Close()
		if err := errors.Join(waitErr, c.cleanupErr); err != nil && !c.stopping.Load() {
			if text := strings.TrimSpace(output.String()); text != "" {
				err = fmt.Errorf("%w; output: %s", err, text)
			}
			m.report(fmt.Errorf("OSD process exited: %w", err))
		}
		close(c.done)
	}()
	if _, err := io.WriteString(stdin, message+"\n"); err != nil {
		return errors.Join(fmt.Errorf("write OSD text: %w", err), m.stopLocked())
	}
	return nil
}

// Clear and Close join the owned direct child and its pipe workers. Failed
// cleanup is retained and prevents a replacement from silently overlapping it.
func (m *Manager) Clear() error { m.mu.Lock(); defer m.mu.Unlock(); return m.stopLocked() }
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.stopLocked()
}
