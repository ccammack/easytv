//go:build linux

package osd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type helperRecord struct {
	PID, PGID, Child int
	Text             string
	Args             []string
}

func TestMain(m *testing.M) {
	if mode := os.Getenv("EASYTV_OSD_TEST_HELPER"); mode != "" {
		os.Exit(runHelper(mode))
	}
	os.Exit(m.Run())
}

func runHelper(mode string) int {
	dir := os.Getenv("EASYTV_OSD_TEST_DIR")
	if mode == "child" {
		if err := os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			return 2
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	terminated := make(chan os.Signal, 1)
	if mode == "ignore" {
		signal.Ignore(syscall.SIGTERM)
	} else {
		signal.Notify(terminated, syscall.SIGTERM)
	}
	text, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return 3
	}
	pgid, err := syscall.Getpgid(0)
	if err != nil {
		return 4
	}
	record := helperRecord{PID: os.Getpid(), PGID: pgid, Text: strings.TrimSuffix(text, "\n"), Args: os.Args[1:]}
	var child *exec.Cmd
	if mode == "tree" {
		child = exec.Command(os.Args[0])
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "EASYTV_OSD_TEST_HELPER=") {
				child.Env = append(child.Env, item)
			}
		}
		child.Env = append(child.Env, "EASYTV_OSD_TEST_HELPER=child")
		if err := child.Start(); err != nil {
			return 5
		}
		record.Child = child.Process.Pid
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "child.pid")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				child.Process.Kill()
				child.Wait()
				return 6
			}
			time.Sleep(time.Millisecond)
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return 7
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", record.PID)), data, 0600); err != nil {
		return 8
	}
	if mode == "natural" {
		return 0
	}
	if mode == "failure" {
		fmt.Fprintln(os.Stderr, "fake overlay failed")
		return 23
	}
	if mode == "ignore" {
		for {
			time.Sleep(time.Hour)
		}
	}
	<-terminated
	if child != nil {
		if err := child.Wait(); err == nil {
			return 9
		}
	}
	return 0
}

func helperManager(t *testing.T, mode string) (*Manager, string, <-chan error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("EASYTV_OSD_TEST_HELPER", mode)
	t.Setenv("EASYTV_OSD_TEST_DIR", dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reports := make(chan error, 16)
	m, err := New(Config{Executable: executable, Report: func(err error) {
		select {
		case reports <- err:
		default:
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m, dir, reports
}

func recordFor(t *testing.T, dir string, pid int) helperRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var record helperRecord
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)))
		if err == nil && json.Unmarshal(data, &record) == nil {
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper %d did not publish record", pid)
		}
		time.Sleep(time.Millisecond)
	}
}

func assertReaped(t *testing.T, c *instance) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("child cleanup incomplete")
	}
	if err := c.cmd.Process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("owned child not reaped: %v", err)
	}
}

func TestReplacementUsesOwnGroupAndReferenceArguments(t *testing.T) {
	m, dir, _ := helperManager(t, "normal")
	if err := m.Show("Movies (1940s)"); err != nil {
		t.Fatal(err)
	}
	first := m.current
	record := recordFor(t, dir, first.cmd.Process.Pid)
	want := []string{"--position", "0", "--x-offset", "32", "--y-offset", "32", "--padding", "16", "--transparency", "0", "--font", "monospace 24", "--back-color", "black", "--fore-color", "white", "--fade-in", "0", "--fade-full", "5000", "--fade-out", "0", "--lines", "1"}
	if record.PGID != record.PID || record.Text != "Movies (1940s)" || !reflect.DeepEqual(record.Args, want) {
		t.Fatalf("record=%+v", record)
	}
	start := time.Now()
	if err := m.Show("Scrapbook"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("replacement waited for display timeout")
	}
	assertReaped(t, first)
	second := m.current
	if second == first {
		t.Fatal("old instance reused")
	}
	if got := recordFor(t, dir, second.cmd.Process.Pid); got.Text != "Scrapbook" {
		t.Fatal(got)
	}
	if err := m.Show(""); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, second)
	if m.current != nil {
		t.Fatal("clear retained instance")
	}
}

func TestClearEscalatesForTERMResistantChild(t *testing.T) {
	m, dir, _ := helperManager(t, "ignore")
	if err := m.Show("resistant"); err != nil {
		t.Fatal(err)
	}
	c := m.current
	recordFor(t, dir, c.cmd.Process.Pid)
	if err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, c)
	if status, ok := c.cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("resistant helper was not killed with SIGKILL")
	}
}

func TestClearSignalsWholeOwnedGroup(t *testing.T) {
	m, dir, _ := helperManager(t, "tree")
	if err := m.Show("tree"); err != nil {
		t.Fatal(err)
	}
	c := m.current
	record := recordFor(t, dir, c.cmd.Process.Pid)
	if record.Child <= 0 {
		t.Fatal("helper did not start descendant")
	}
	if err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, c)
	// Parent waits/reaps the descendant after both receive the group TERM.
	if err := syscall.Kill(record.Child, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("descendant retained: pid=%d err=%v", record.Child, err)
	}
}

func TestNaturalExitAndFailureAreReaped(t *testing.T) {
	for _, mode := range []string{"natural", "failure"} {
		t.Run(mode, func(t *testing.T) {
			m, dir, reports := helperManager(t, mode)
			if err := m.Show(mode); err != nil {
				t.Fatal(err)
			}
			c := m.current
			recordFor(t, dir, c.cmd.Process.Pid)
			assertReaped(t, c)
			if mode == "failure" {
				select {
				case err := <-reports:
					if !strings.Contains(err.Error(), "fake overlay failed") {
						t.Fatal(err)
					}
				default:
					t.Fatal("natural process failure was not reported")
				}
			}
			if err := m.Clear(); err != nil {
				t.Fatal(err)
			}
			if err := m.Clear(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidTextDoesNotReplaceAndClosePreventsRestart(t *testing.T) {
	m, dir, _ := helperManager(t, "normal")
	if err := m.Show("valid"); err != nil {
		t.Fatal(err)
	}
	c := m.current
	recordFor(t, dir, c.cmd.Process.Pid)
	for _, text := range []string{"two\nlines", "carriage\rreturn", strings.Repeat("x", 4096)} {
		if err := m.Show(text); err == nil || m.current != c {
			t.Fatal("invalid text changed overlay")
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, c)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Show("closed"); err == nil {
		t.Fatal("closed manager restarted")
	}
}

func TestFailedExecutableDoesNotLeaveInstance(t *testing.T) {
	if _, err := New(Config{Executable: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("missing executable accepted")
	}
	path := filepath.Join(t.TempDir(), "removed-helper")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(Config{Executable: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := m.Show("missing"); err == nil || m.current != nil {
		t.Fatal("failed start retained instance")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentReplacementAndCloseJoinAllChildren(t *testing.T) {
	m, dir, _ := helperManager(t, "normal")
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) { defer group.Done(); failures <- m.Show(fmt.Sprintf("channel %d", i)) }(i)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		pid, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(path), ".json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("helper retained: %d / %v", pid, err)
		}
	}
}

func TestClearDoesNotSignalUnrelatedHelper(t *testing.T) {
	m, dir, _ := helperManager(t, "normal")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := exec.Command(executable)
	unrelated.Stdin = strings.NewReader("unrelated\n")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { unrelated.Process.Kill(); unrelated.Wait() }()
	recordFor(t, dir, unrelated.Process.Pid)
	if err := m.Show("owned"); err != nil {
		t.Fatal(err)
	}
	c := m.current
	recordFor(t, dir, c.cmd.Process.Pid)
	if err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, c)
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated helper affected: %v", err)
	}
}
