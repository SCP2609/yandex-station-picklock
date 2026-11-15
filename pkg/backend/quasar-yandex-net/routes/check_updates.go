package routes

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

func CheckUpdates(update *ota.Update) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, err := url.ParseQuery(r.URL.RawQuery)
		// Duplicate eligibility parameters must never advertise an update.
		if err != nil || len(query["version"]) != 1 || len(query["platform"]) > 1 {
			AllowUpdate(w, r)
			return
		}
		manifest, err := update.Check(query.Get("version"), query.Get("platform"))
		if err != nil {
			slog.Warn("OTA image unavailable", "error", err)
		}
		httpresponse.JSON(w, r, http.StatusOK, manifest)
	})
}

func AllowUpdate(w http.ResponseWriter, r *http.Request) {
	httpresponse.JSON(w, r, http.StatusOK, ota.Manifest{})
}
