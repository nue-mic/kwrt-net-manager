package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// pathID returns the chi URL param "id" or empty string.
func pathID(r *http.Request) string {
	return chi.URLParam(r, "id")
}

// pathName returns the chi URL param "name" or empty string.
func pathName(r *http.Request) string {
	return chi.URLParam(r, "name")
}

// decodeJSON parses the request body into dst. A 400 is written and false
// returned on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "invalid JSON body: "+err.Error(), nil)
		return false
	}
	return true
}
