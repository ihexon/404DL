package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

var fallbackRequestID uint64

func NewRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	n := atomic.AddUint64(&fallbackRequestID, 1)
	return fmt.Sprintf("%x-%x", time.Now().UnixNano(), n)
}

func RequestIDFromHTTP(r *http.Request) string {
	if r != nil {
		if value := strings.TrimSpace(r.Header.Get(RequestIDHeader)); value != "" {
			return value
		}
	}
	return NewRequestID()
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func ContextFields(ctx context.Context) logrus.Fields {
	requestID := RequestID(ctx)
	if requestID == "" {
		return logrus.Fields{}
	}
	return logrus.Fields{"request_id": requestID}
}

func MergeFields(ctx context.Context, fields logrus.Fields) logrus.Fields {
	out := ContextFields(ctx)
	for key, value := range fields {
		out[key] = value
	}
	return out
}

func HTTPRequestFields(r *http.Request, requestID string) logrus.Fields {
	fields := logrus.Fields{"request_id": requestID}
	if r == nil {
		return fields
	}
	fields["method"] = r.Method
	fields["path"] = r.URL.Path
	fields["remote_addr"] = r.RemoteAddr
	if userAgent := strings.TrimSpace(r.UserAgent()); userAgent != "" {
		fields["user_agent"] = Truncate(userAgent, 160)
	}
	return fields
}

func DurationMillis(duration time.Duration) int64 {
	return duration.Milliseconds()
}

func Truncate(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
