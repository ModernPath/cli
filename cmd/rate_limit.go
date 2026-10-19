package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type serverLimitingError struct {
	retryAfter int // seconds; 0 when the server named none
}

func (e serverLimitingError) Error() string {
	if e.retryAfter <= 0 {
		return "The ModernPath server is limiting requests; retry shortly."
	}
	return fmt.Sprintf("The ModernPath server is limiting requests; retry after %d s.", e.retryAfter)
}

// retryAfterSeconds reads Retry-After as seconds or as an HTTP date.
func retryAfterSeconds(h string, now time.Time) int {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(h); err == nil && n >= 0 {
		return n
	}
	if at, err := http.ParseTime(h); err == nil {
		if d := at.Sub(now); d > 0 {
			return int((d + time.Second - 1) / time.Second)
		}
	}
	return 0
}

// coreErrorBody reports a body core wrote: a JSON object whose error is an
// object. A gateway's plain text or a proxy's {"error": "…"} is not.
func coreErrorBody(raw []byte) bool {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return false
	}
	_, ok := body["error"].(map[string]any)
	return ok
}
