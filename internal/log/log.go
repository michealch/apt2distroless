// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarning
	LevelError
)

func ParseLevel(s string) (Level, error) {
	switch s {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warning":
		return LevelWarning, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level %q: use debug|info|warning|error", s)
	}
}

type Logger struct {
	level  Level
	format string // "text" | "json"
	out    io.Writer
	errOut io.Writer
}

func New(level Level, format string) *Logger {
	return &Logger{level: level, format: format, out: os.Stdout, errOut: os.Stderr}
}

func (l *Logger) log(lvl Level, msg string) {
	if lvl < l.level {
		return
	}
	w := l.out
	if lvl >= LevelError {
		w = l.errOut
	}
	if l.format == "json" {
		r := map[string]string{
			"time":  time.Now().UTC().Format(time.RFC3339),
			"level": levelName(lvl),
			"msg":   msg,
		}
		b, _ := json.Marshal(r)
		_, _ = fmt.Fprintln(w, string(b))
	} else {
		_, _ = fmt.Fprintf(w, "[%s] %s: %s\n", time.Now().UTC().Format("2006-01-02 15:04:05"), levelName(lvl), msg)
	}
}

func levelName(l Level) string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarning:
		return "WARNING"
	case LevelError:
		return "ERROR"
	}
	return "INFO"
}

func (l *Logger) Debug(format string, args ...any) { l.log(LevelDebug, fmt.Sprintf(format, args...)) }
func (l *Logger) Info(format string, args ...any)  { l.log(LevelInfo, fmt.Sprintf(format, args...)) }
func (l *Logger) Warn(format string, args ...any)  { l.log(LevelWarning, fmt.Sprintf(format, args...)) }
func (l *Logger) Error(format string, args ...any) { l.log(LevelError, fmt.Sprintf(format, args...)) }
