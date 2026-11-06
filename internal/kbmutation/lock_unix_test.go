//go:build !windows

package kbmutation

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestSharedWritersAndExclusiveTransfer(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(dir, "kb", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(dir, "kb", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "kb", true); err != ErrBusy {
		t.Fatalf("exclusive with writers: %v", err)
	}
	other, err := Acquire(dir, "other", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := other(); err != nil {
		t.Fatal(err)
	}
	if err := first(); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "kb", true); err != ErrBusy {
		t.Fatalf("exclusive with one writer: %v", err)
	}
	if err := second(); err != nil {
		t.Fatal(err)
	}
	move, err := Acquire(dir, "kb", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "kb", false); err != ErrBusy {
		t.Fatalf("writer during transfer: %v", err)
	}
	if err := move(); err != nil {
		t.Fatal(err)
	}
}

func TestLockReleasedWhenProcessDies(t *testing.T) {
	if dir := os.Getenv("WEKNORA_LOCK_TEST_CHILD_DIR"); dir != "" {
		_, err := Acquire(dir, "kb", false)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		var line string
		_, _ = fmt.Scanln(&line)
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockReleasedWhenProcessDies$")
	cmd.Env = append(os.Environ(), "WEKNORA_LOCK_TEST_CHILD_DIR="+dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("child did not lock: %q %v", line, err)
	}
	if _, err := Acquire(dir, "kb", true); err != ErrBusy {
		t.Fatalf("lock held by child: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	release, err := Acquire(dir, "kb", true)
	if err != nil {
		t.Fatalf("lock after child exit: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
