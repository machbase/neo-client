package util

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
	"gopkg.in/natefinch/lumberjack.v2"
)

func InitLogger(filename string, level string, maxSize, maxAge, maxBackups int, compress bool) {
	var lw io.Writer
	opt := &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: "2006-01-02 15:04:05 -0700",
		AddSource:  false,
		NoColor:    true,
	}
	switch strings.ToUpper(level) {
	case "DEBUG":
		opt.Level = slog.LevelDebug
	case "INFO":
		opt.Level = slog.LevelInfo
	case "WARN":
		opt.Level = slog.LevelWarn
	case "ERROR":
		opt.Level = slog.LevelError
	}

	if filename == "" {
		// disable logging
		lw = io.Discard
	} else if filename == "-" {
		opt.NoColor = false
		lw = os.Stdout
	} else {
		lw = &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    maxSize,
			MaxAge:     maxAge,
			MaxBackups: maxBackups,
			LocalTime:  true,
			Compress:   compress,
		}
	}
	ll := tint.NewHandler(lw, opt)
	slog.SetDefault(slog.New(ll))
}
