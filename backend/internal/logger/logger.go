package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var (
	level     Level = INFO
	levelMu   sync.RWMutex
	buffer    []LogEntry
	bufferMu  sync.Mutex
	maxBuffer = 1000
)

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

func SetLevel(l string) {
	levelMu.Lock()
	defer levelMu.Unlock()
	switch strings.ToUpper(l) {
	case "DEBUG":
		level = DEBUG
	case "INFO":
		level = INFO
	case "WARN", "WARNING":
		level = WARN
	case "ERROR":
		level = ERROR
	default:
		level = INFO
	}
}

func GetLevel() string {
	levelMu.RLock()
	defer levelMu.RUnlock()
	switch level {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	}
	return "INFO"
}

func shouldLog(l Level) bool {
	levelMu.RLock()
	defer levelMu.RUnlock()
	return l >= level
}

func logf(l Level, format string, v ...interface{}) {
	if !shouldLog(l) {
		return
	}

	levelStr := "INFO"
	switch l {
	case DEBUG:
		levelStr = "DEBUG"
	case INFO:
		levelStr = "INFO"
	case WARN:
		levelStr = "WARN"
	case ERROR:
		levelStr = "ERROR"
	}

	msg := fmt.Sprintf(format, v...)
	log.Printf("[%s] %s", levelStr, msg)

	// store in buffer
	entry := LogEntry{
		Time:    time.Now(),
		Level:   levelStr,
		Message: msg,
	}
	bufferMu.Lock()
	buffer = append(buffer, entry)
	if len(buffer) > maxBuffer {
		buffer = buffer[len(buffer)-maxBuffer:]
	}
	bufferMu.Unlock()
}

func Debugf(format string, v ...interface{}) {
	logf(DEBUG, format, v...)
}

func Infof(format string, v ...interface{}) {
	logf(INFO, format, v...)
}

func Warnf(format string, v ...interface{}) {
	logf(WARN, format, v...)
}

func Errorf(format string, v ...interface{}) {
	logf(ERROR, format, v...)
}

func GetRecent(limit int) []LogEntry {
	bufferMu.Lock()
	defer bufferMu.Unlock()
	if limit <= 0 || limit > len(buffer) {
		limit = len(buffer)
	}
	// return copy of last N
	start := len(buffer) - limit
	res := make([]LogEntry, limit)
	copy(res, buffer[start:])
	return res
}