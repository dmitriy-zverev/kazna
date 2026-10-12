package respond

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxBodyBytes caps request bodies so a client cannot exhaust memory.
const MaxBodyBytes = 1 << 20 // 1 MiB

// DecodeJSON reads a JSON request body into dst. On failure it writes the
// normalized error envelope and returns false, so handlers read as:
//
//	if !respond.DecodeJSON(w, r, &req) {
//		return
//	}
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			Error(w, r, http.StatusRequestEntityTooLarge, CodeInvalidRequest, "request body is too large", false)
			return false
		}
		Error(w, r, http.StatusBadRequest, CodeInvalidRequest, "invalid JSON body: "+err.Error(), false)
		return false
	}

	// Reject a second JSON document in the same body.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		Error(w, r, http.StatusBadRequest, CodeInvalidRequest, "request body must contain a single JSON object", false)
		return false
	}
	return true
}
