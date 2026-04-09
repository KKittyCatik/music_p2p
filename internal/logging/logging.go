// Package logging provides a global structured logger backed by go.uber.org/zap.
package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the global sugared logger. Init must be called before use.
var Logger *zap.SugaredLogger

func init() {
	// Provide a safe default so packages can log before Init is called.
	l, _ := zap.NewProduction()
	Logger = l.Sugar()
}

// Init initialises the global logger with the given log level string
// ("debug", "info", "warn", "error").
func Init(level string) {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(parseLevel(level))
	cfg.OutputPaths = []string{"stdout"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	l, err := cfg.Build()
	if err != nil {
		// Fallback: keep the existing logger.
		return
	}
	Logger = l.Sugar()
}

// parseLevel converts a string to a zapcore.Level, defaulting to InfoLevel.
func parseLevel(s string) zapcore.Level {
	switch s {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
