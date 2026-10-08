package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLoggerLevels(t *testing.T) {
	for level, want := range map[string]slog.Level{
		"debug":   slog.LevelDebug,
		" Info ":  slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"WARNING": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
	} {
		logger, closeFn, err := newLogger("null", level)
		if err != nil {
			t.Fatal(err)
		}
		if !logger.Enabled(context.Background(), want) || (want > slog.LevelDebug && logger.Enabled(context.Background(), want-4)) {
			t.Errorf("level %q: logger does not log exactly from %v on", level, want)
		}
		if err := closeFn(); err != nil {
			t.Errorf("close for null: %v", err)
		}
	}
}

func TestNewLoggerAppendsToFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "s0meter.log")

	for _, msg := range []string{"first", "second"} {
		logger, closeFn, err := newLogger(file, "info")
		if err != nil {
			t.Fatal(err)
		}
		logger.Info(msg)
		if err := closeFn(); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "msg=first") || !strings.Contains(string(data), "msg=second") {
		t.Errorf("log file does not hold both runs:\n%s", data)
	}
}

func TestNewLoggerUnwritableFile(t *testing.T) {
	if _, _, err := newLogger(filepath.Join(t.TempDir(), "missing", "dir", "x.log"), "info"); err == nil {
		t.Error("expected an error for a file in a missing directory")
	}
}
