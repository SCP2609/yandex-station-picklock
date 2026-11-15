package routes

import (
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

func OTAUpdate(update *ota.Update) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			httpresponse.Error(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		image, err := update.Open()
		if err != nil {
			slog.Warn("OTA download unavailable", "error", err)
			ImageUnavailable(w, r)
			return
		}
		defer image.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Accept-Ranges", "bytes")
		// ServeContent owns HEAD, range validation and resumable transfers.
		http.ServeContent(w, r, path.Base(update.DownloadPath()), time.Time{}, image)
	})
}

func ImageUnavailable(w http.ResponseWriter, r *http.Request) {
	httpresponse.Error(w, r, http.StatusNotFound, "image unavailable")
}
