package httpresponse

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func JSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func Error(w http.ResponseWriter, r *http.Request, status int, message string) {
	JSON(w, r, status, map[string]string{"error": message})
}

func UpstreamDisabled(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusBadGateway, "upstream disabled")
}
