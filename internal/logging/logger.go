package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	debugLogger *log.Logger
	logFile     *os.File
)

// InitLogger initializes the debug logger to write to a file next to the executable
func InitLogger() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	logPath := filepath.Join(exeDir, fmt.Sprintf("rag-debug-%s.log", time.Now().Format("2006-01-02")))

	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	debugLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
	debugLogger.Printf("=== RAG Chat Debug Log Started ===")

	return nil
}

// Debug logs a debug message
func Debug(format string, v ...interface{}) {
	if debugLogger != nil {
		debugLogger.Printf("[DEBUG] "+format, v...)
	}
}

// Info logs an info message
func Info(format string, v ...interface{}) {
	if debugLogger != nil {
		debugLogger.Printf("[INFO] "+format, v...)
	}
}

// Error logs an error message
func Error(format string, v ...interface{}) {
	if debugLogger != nil {
		debugLogger.Printf("[ERROR] "+format, v...)
	}
}

// Close closes the log file
func Close() {
	if logFile != nil {
		debugLogger.Printf("=== RAG Chat Debug Log Ended ===")
		logFile.Close()
	}
}
