// Package logx filters debug and info lines by the log_level from the config.
// Warnings and errors keep using log.Printf directly and are always written.
package logx

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

const (
	levelDebug int32 = iota
	levelInfo
	levelWarn
	levelError
)

var level atomic.Int32

func init() { level.Store(levelInfo) }

// SetLevel applies a log_level value: debug, info, warn or error (any case).
// Empty means info; an unknown value also falls back to info and is reported.
func SetLevel(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		level.Store(levelDebug)
	case "info", "":
		level.Store(levelInfo)
	case "warn":
		level.Store(levelWarn)
	case "error":
		level.Store(levelError)
	default:
		level.Store(levelInfo)
		return fmt.Errorf("unknown log_level %q (want debug, info, warn or error); using info", s)
	}
	return nil
}

// Debugf logs with a "[DEBUG] " prefix when the level is debug.
func Debugf(format string, args ...any) {
	if level.Load() <= levelDebug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// Infof logs with an "[INFO] " prefix when the level is debug or info.
func Infof(format string, args ...any) {
	if level.Load() <= levelInfo {
		log.Printf("[INFO] "+format, args...)
	}
}
