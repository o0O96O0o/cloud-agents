package logger

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Config controls log level and output format.
type Config struct {
	Level  string `yaml:"level"`  // debug | info | warn | error (default: info)
	Format string `yaml:"format"` // json | text (default: json)
}

var (
	mu  sync.RWMutex
	def *slog.Logger
)

func init() {
	def = New(Config{})
}

// New creates a *slog.Logger from Config.
func New(cfg Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}
	var h slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// Init sets the package-level default logger and redirects stdlib log output
// through the new handler (slog.SetDefault wires log.Printf et al. in Go 1.21+).
func Init(cfg Config) *slog.Logger {
	l := New(cfg)
	mu.Lock()
	def = l
	mu.Unlock()
	slog.SetDefault(l)
	return l
}

// Default returns the package-level default logger.
func Default() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return def
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
