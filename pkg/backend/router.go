package backend

import (
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
	oauthyandexru "github.com/fr33dman/yandex-station-picklock/pkg/backend/oauth-yandex-ru"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
	quasaryandexnet "github.com/fr33dman/yandex-station-picklock/pkg/backend/quasar-yandex-net"
	quasaryandexru "github.com/fr33dman/yandex-station-picklock/pkg/backend/quasar-yandex-ru"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/routes"
)

const maxRequestBody = 8 << 20

// NewRouter assembles local routes only; nil disables OTA for this instance.
func NewRouter(update *ota.Update) http.Handler {
	domains := http.NewServeMux()

	// Add handler for oauth.yandex.ru (token retrieve)
	domains.Handle("oauth.yandex.ru/", oauthyandexru.NewRouter())

	// Add handler for quasar.yandex.net and scbh.yandex.net
	quasar := quasaryandexnet.NewRouter(update)
	domains.Handle("quasar.yandex.net/", quasar)
	domains.Handle("scbh.yandex.net/", quasar)

	// Add handler for quasar.yandex.ru
	domains.Handle("quasar.yandex.ru/", quasaryandexru.NewRouter(update))

	// Add handler for unknown paths
	domains.HandleFunc("/", httpresponse.UpstreamDisabled)

	mux := http.NewServeMux()

	// Connectivity probes are host-independent, like the original backend.
	mux.HandleFunc("/health", routes.Health)
	mux.HandleFunc("/generate_204", routes.Generate204)
	mux.HandleFunc("/dev/static/speed/200kb", routes.Speed200KB)
	// OTA URLs point to the local IP, so downloads cannot depend on Host.
	mux.Handle("/ota/", quasar)
	mux.Handle("/", domains)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.ContentLength > maxRequestBody {
			httpresponse.Error(w, r, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		if r.Body != nil {
			// Discard bodies without buffering; chunked requests have the same limit.
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				status, message := http.StatusBadRequest, "invalid request body"
				if _, ok := err.(*http.MaxBytesError); ok {
					status, message = http.StatusRequestEntityTooLarge, "body too large"
				}
				httpresponse.Error(w, r, status, message)
				return
			}
		}
		host := r.Host
		if name, _, err := net.SplitHostPort(host); err == nil {
			host = name
		}
		request := r.Clone(r.Context())
		request.Host = strings.ToLower(strings.TrimSuffix(host, "."))
		mux.ServeHTTP(w, request)
	})
}
