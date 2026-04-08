package routes

import (
	"io"
	"net/http"
	"os"

	"roboticus/internal/core"
)

// GetConfigRaw returns the raw TOML config file content as text/plain.
func GetConfigRaw() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := core.ReadConfigRaw()
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "config file not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// UpdateConfigRaw writes new TOML content to the config file.
func UpdateConfigRaw() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		if len(body) == 0 {
			writeError(w, http.StatusBadRequest, "empty body")
			return
		}

		writtenPath, err := core.WriteConfigRaw(body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "path": writtenPath})
	}
}
