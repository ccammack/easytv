package mpvplayer

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/mpvipc"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Player has one serialized owner. Stop waits for process exit.
type Config struct {
	Prepare        func(context.Context, string) (preparedmedia.Result, error)
	Executable     string
	OutputArgs     []string
	StartupTimeout time.Duration
	StopTimeout    time.Duration
}
type Player struct {
	config     Config
	cmd        *exec.Cmd
	client     *mpvipc.Client
	done       chan struct{}
	waitErr    error
	dir        string
	log        *os.File
	generation uint64
}

func New(config Config) *Player {
	if config.Executable == "" {
		config.Executable = "mpv"
	}
	if config.StartupTimeout <= 0 {
		config.StartupTimeout = 5 * time.Second
	}
	if config.StopTimeout <= 0 {
		config.StopTimeout = time.Second
	}
	config.OutputArgs = append([]string(nil), config.OutputArgs...)
	return &Player{config: config}
}
func (p *Player) Client() *mpvipc.Client { return p.client }
func (p *Player) Generation() uint64     { return p.generation }
func (p *Player) Running() bool {
	if p.cmd == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func (p *Player) cleanup() {
	if p.client != nil {
		_ = p.client.Close()
	}
	if p.log != nil {
		_ = p.log.Close()
	}
	if p.dir != "" {
		_ = os.RemoveAll(p.dir)
	}
	p.cmd = nil
	p.client = nil
	p.log = nil
	p.dir = ""
}
func (p *Player) Start(file string, offset int64, generation uint64) (result error) {
	if p.cmd != nil {
		return errors.New("previous player must be stopped before start")
	}
	if file == "" || offset < 0 {
		return errors.New("media path and nonnegative offset required")
	}
	absolute, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	original := absolute
	if p.config.Prepare != nil {
		prepared, prepareErr := p.config.Prepare(context.Background(), original)
		if prepareErr != nil {
			return fmt.Errorf("prepare media: %w", prepareErr)
		}
		if prepared.PlayPath == "" {
			return errors.New("empty prepared playback path")
		}
		absolute, err = filepath.Abs(prepared.PlayPath)
		if err != nil {
			return err
		}
	}
	p.dir, err = os.MkdirTemp("/tmp", "etv-player-")
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			if p.cmd != nil {
				_ = p.cmd.Process.Kill()
				<-p.done
			}
			if p.log != nil {
				data, _ := os.ReadFile(p.log.Name())
				if len(data) > 4096 {
					data = data[len(data)-4096:]
				}
				if len(data) > 0 {
					result = fmt.Errorf("%w; mpv output: %s", result, data)
				}
			}
			p.cleanup()
		}
	}()
	socket := filepath.Join(p.dir, "ipc.sock")
	p.log, err = os.Create(filepath.Join(p.dir, "mpv.log"))
	if err != nil {
		return err
	}
	args := []string{"--no-config", "--no-terminal", "--idle=no", "--no-input-default-bindings", "--input-ipc-server=" + socket}
	args = append(args, mediaArguments(original, offset)...)
	args = append(args, p.config.OutputArgs...)
	args = append(args, "--", absolute)
	cmd := exec.Command(p.config.Executable, args...)
	cmd.Stdout = p.log
	cmd.Stderr = p.log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch mpv: %w", err)
	}
	p.cmd = cmd
	p.done = make(chan struct{})
	p.waitErr = nil
	go func() { p.waitErr = cmd.Wait(); close(p.done) }()
	ctx, cancel := context.WithTimeout(context.Background(), p.config.StartupTimeout)
	defer cancel()
	var conn net.Conn
	for {
		select {
		case <-p.done:
			return fmt.Errorf("mpv exited during startup: %v", p.waitErr)
		default:
		}
		conn, err = net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("connect mpv IPC: %w", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	// Reference startup readiness is IPC connectivity, not file-loaded.
	p.client = mpvipc.New(conn)
	p.generation = generation
	success = true
	return nil
}

// Stop matches the reference: kill our child, reap it, then clean resources.
func (p *Player) Stop() error {
	if p.cmd == nil {
		return nil
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill mpv: %w", err)
	}
	<-p.done
	p.cleanup()
	return nil
}
