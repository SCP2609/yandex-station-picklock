package quasaryandexnet

import (
	"net/http"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/quasar-yandex-net/routes"
)

func NewRouter(update *ota.Update) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/register", routes.Register)
	mux.HandleFunc("/get_sync_info", routes.Sync)
	mux.HandleFunc("/sync", routes.Sync)
	// Older clients use /v2/check_updates with the same wire format.
	mux.Handle("/check_updates", routes.CheckUpdates(update))
	mux.Handle("/v2/check_updates", routes.CheckUpdates(update))
	mux.HandleFunc("/allow_update", routes.AllowUpdate)
	mux.HandleFunc("/ota/", routes.ImageUnavailable)
	mux.HandleFunc("/ota-test/no-image.zip", routes.ImageUnavailable)
	if update.DownloadPath() != "" {
		mux.Handle(update.DownloadPath(), routes.OTAUpdate(update))
	}
	mux.HandleFunc("/", httpresponse.UpstreamDisabled)
	return mux
}
