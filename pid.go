package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var pidFilePath string

func init() {
	home, err := os.UserHomeDir()
	if err == nil {
		pidFilePath = filepath.Join(home, ".config", "ocgo2cli", "daemon.pid")
	}
}

func writePID() error {
	if pidFilePath == "" {
		return nil
	}
	dir := filepath.Dir(pidFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}
	return os.WriteFile(pidFilePath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
}

func removePID() {
	if pidFilePath == "" {
		return
	}
	os.Remove(pidFilePath)
}

func readPID() (int, error) {
	if pidFilePath == "" {
		return 0, fmt.Errorf("pid file path not set")
	}
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("daemon not running")
		}
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}
	return pid, nil
}

func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(os.Signal(nil))
	return err == nil
}
