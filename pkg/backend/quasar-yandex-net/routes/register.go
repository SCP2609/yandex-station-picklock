package routes

import (
	"net/http"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
)

func Register(w http.ResponseWriter, r *http.Request) {
	httpresponse.JSON(w, r, http.StatusOK, map[string]string{
		"message": "allvalid",
		"status":  "ok",
	})
}
