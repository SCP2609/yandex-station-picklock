package routes

import (
	"net/http"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/quasar"
)

func Sync(w http.ResponseWriter, r *http.Request) {
	httpresponse.JSON(w, r, http.StatusOK, quasar.Sync(r.URL.Query().Get("platform")))
}
