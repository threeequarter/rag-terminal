package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LogLevel int

const (
	LogLevelNone LogLevel = iota
	LogLevelError
	LogLevelInfo
	LogLevelDebug
)

var (
	logger   *log.Logger
	logFile  *os.File
	logLevel LogLevel = LogLevelNone
)

// InitLogger initializes the logger based on RT_LOGS environment variable
func InitLogger() error {
	rtLogs := os.Getenv("RT_LOGS")
	if rtLogs == "" {
		// No logging enabled
		return nil
	}

	// Parse log level
	switch strings.ToLower(rtLogs) {
	case "debug":
		logLevel = LogLevelDebug
	case "info":
		logLevel = LogLevelInfo
	case "error":
		logLevel = LogLevelError
	default:
		// Invalid value, disable logging
		return nil
	}

	// Get user home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Create logs directory
	logsDir := filepath.Join(homeDir, ".rag-terminal", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Create log file with date
	logPath := filepath.Join(logsDir, fmt.Sprintf("rag-%s.log", time.Now().Format("2006-01-02")))

	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("=== RAG Terminal Log Started (Level: %s) ===", rtLogs)

	return nil
}

// Debug logs a debug message (only if RT_LOGS=debug)
func Debug(format string, v ...interface{}) {
	if logger != nil && logLevel >= LogLevelDebug {
		logger.Printf("[DEBUG] "+format, v...)
	}
}

// Info logs an info message (if RT_LOGS=debug or RT_LOGS=info)
func Info(format string, v ...interface{}) {
	if logger != nil && logLevel >= LogLevelInfo {
		logger.Printf("[INFO] "+format, v...)
	}
}

// Error logs an error message (if RT_LOGS is set to any value)
func Error(format string, v ...interface{}) {
	if logger != nil && logLevel >= LogLevelError {
		logger.Printf("[ERROR] "+format, v...)
	}
}

// Close closes the log file
func Close() {
	if logFile != nil {
		logger.Printf("=== RAG Terminal Log Ended ===")
		logFile.Close()
	}
}
