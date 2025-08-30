package scanner

import "log"

// LogLevel represents logging severity.
type LogLevel int

const (
	LevelInfo LogLevel = iota
	LevelWarn
)

var currentLevel LogLevel = LevelInfo

// SetLogLevel updates the current logging level.
func SetLogLevel(l LogLevel) { currentLevel = l }

// Infof logs formatted info messages when the level allows.
func Infof(format string, args ...interface{}) {
	if currentLevel <= LevelInfo {
		log.Printf("[INFO] "+format, args...)
	}
}

// Warnf logs formatted warning messages when the level allows.
func Warnf(format string, args ...interface{}) {
	if currentLevel <= LevelWarn {
		log.Printf("[WARN] "+format, args...)
	}
}
