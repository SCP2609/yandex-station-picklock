package routes

import (
	"bytes"
	"net/http"
	"time"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
)

var speedPayload = make([]byte, 200*1024)

func Health(w http.ResponseWriter, r *http.Request) {
	httpresponse.JSON(w, r, http.StatusOK, map[string]bool{"offline": true})
}

func Generate204(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func Speed200KB(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "200kb", time.Time{}, bytes.NewReader(speedPayload))
}
