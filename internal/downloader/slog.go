package downloader

import (
	"context"
	"log/slog"
	"strings"

	log "github.com/sirupsen/logrus"
)

type logrusSlogHandler struct {
	attrs    []slog.Attr
	groups   []string
	minLevel slog.Level
}

func (h logrusSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h logrusSlogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := log.Fields{}
	for _, attr := range h.attrs {
		addSlogAttr(fields, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addSlogAttr(fields, h.groups, attr)
		return true
	})

	entry := log.WithFields(fields)
	switch {
	case record.Level >= slog.LevelError:
		entry.Error(record.Message)
	case record.Level >= slog.LevelWarn:
		entry.Warn(record.Message)
	case record.Level <= slog.LevelDebug:
		entry.Debug(record.Message)
	default:
		entry.Info(record.Message)
	}
	return nil
}

func (h logrusSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logrusSlogHandler{
		attrs:    append(append([]slog.Attr{}, h.attrs...), attrs...),
		groups:   append([]string{}, h.groups...),
		minLevel: h.minLevel,
	}
}

func (h logrusSlogHandler) WithGroup(name string) slog.Handler {
	return logrusSlogHandler{
		attrs:    append([]slog.Attr{}, h.attrs...),
		groups:   append(append([]string{}, h.groups...), name),
		minLevel: h.minLevel,
	}
}

func addSlogAttr(fields log.Fields, groups []string, attr slog.Attr) {
	if attr.Key == "" {
		return
	}

	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(append(append([]string{}, groups...), attr.Key), ".")
	}
	fields[key] = attr.Value.Resolve().Any()
}
