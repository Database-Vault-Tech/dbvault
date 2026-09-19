// Package httpx contains JSON response helpers shared by every handler.
//
// Response envelope:
//
//	success: {"data": ...}            (lists may add "meta")
//	error:   {"error": {"code", "message", "fields"?, "request_id"}}
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

const maxBodyBytes = 1 << 20

// JSON writes v wrapped in {"data": v}.
func JSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, map[string]any{"data": v})
}

// List writes {"data": items, "meta": meta}.
func List(w http.ResponseWriter, items any, meta map[string]any) {
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": meta})
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes err as a JSON error. Unexpected errors are logged with full
// detail server-side and returned to the client as a generic 500.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	e, ok := apperr.As(err)
	if !ok {
		if db.IsNotFound(err) {
			e = apperr.NotFound("Resource")
		} else {
			logging.FromContext(r.Context()).Error("request failed", "error", err.Error())
			e = apperr.Internal()
		}
	}
	body := map[string]any{
		"code":       e.Code,
		"message":    e.Message,
		"request_id": reqctx.MetaFrom(r.Context()).RequestID,
	}
	if len(e.Fields) > 0 {
		body["fields"] = e.Fields
	}
	writeJSON(w, e.Status, map[string]any{"error": body})
}

// Decode reads a JSON body into dst, rejecting unknown fields and oversize bodies.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return apperr.BadRequest("Request body is required.")
		case errors.As(err, &maxErr):
			return apperr.New(http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			return apperr.BadRequest("Unknown field " + strings.TrimPrefix(err.Error(), "json: unknown field ") + ".")
		default:
			return apperr.BadRequest("Request body must be valid JSON.")
		}
	}
	if dec.More() {
		return apperr.BadRequest("Request body must contain a single JSON object.")
	}
	return nil
}

// QueryInt reads a bounded integer query parameter.
func QueryInt(r *http.Request, key string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// IsUUID reports whether s looks like a canonical UUID. Handlers use it to
// reject malformed IDs with 404 before they reach the database.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
