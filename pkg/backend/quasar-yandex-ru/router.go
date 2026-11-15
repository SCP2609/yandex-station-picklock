package quasaryandexru

import (
	"net/http"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
	quasaryandexnet "github.com/fr33dman/yandex-station-picklock/pkg/backend/quasar-yandex-net"
)

func NewRouter(update *ota.Update) http.Handler {
	// Both Quasar domains use the same offline protocol and OTA state.
	return quasaryandexnet.NewRouter(update)
}
