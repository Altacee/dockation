package observability

import (
	"regexp"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Patterns for secret redaction
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|secret|key|token|auth|credential|api_key)[\s]*[=:][\s]*[^\s]+`),
	}

	// Environment variable patterns to redact
	secretEnvKeys = []string{
		"PASSWORD", "SECRET", "KEY", "TOKEN", "AUTH", "CREDENTIAL", "API_KEY",
	}
)

// Logger wraps zap.Logger with secret redaction
type Logger struct {
	*zap.Logger
}

// NewLogger creates a production logger with JSON encoding and secret redaction
func NewLogger(level string) (*Logger, error) {
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zapLevel),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding: "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			FunctionKey:    zapcore.OmitKey,
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{Logger: logger}, nil
}

// RedactString removes secrets from a string
func RedactString(s string) string {
	redacted := s
	for _, pattern := range secretPatterns {
		redacted = pattern.ReplaceAllStringFunc(redacted, func(match string) string {
			parts := strings.SplitN(match, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=***REDACTED***"
			}
			parts = strings.SplitN(match, ":", 2)
			if len(parts) == 2 {
				return parts[0] + ":***REDACTED***"
			}
			return "***REDACTED***"
		})
	}
	return redacted
}

// RedactEnv redacts sensitive environment variables
func RedactEnv(env []string) []string {
	redacted := make([]string, len(env))
	for i, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		shouldRedact := false
		for _, pattern := range secretEnvKeys {
			if strings.Contains(strings.ToUpper(key), pattern) {
				shouldRedact = true
				break
			}
		}
		if shouldRedact {
			redacted[i] = key + "=***REDACTED***"
		} else {
			redacted[i] = e
		}
	}
	return redacted
}

// InfoRedacted logs with automatic secret redaction
func (l *Logger) InfoRedacted(msg string, fields ...zap.Field) {
	redactedFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		if f.Type == zapcore.StringType {
			redactedFields[i] = zap.String(f.Key, RedactString(f.String))
		} else {
			redactedFields[i] = f
		}
	}
	l.Info(RedactString(msg), redactedFields...)
}

// ErrorRedacted logs errors with automatic secret redaction
func (l *Logger) ErrorRedacted(msg string, fields ...zap.Field) {
	redactedFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		if f.Type == zapcore.StringType {
			redactedFields[i] = zap.String(f.Key, RedactString(f.String))
		} else {
			redactedFields[i] = f
		}
	}
	l.Error(RedactString(msg), redactedFields...)
}
