package oauthyandexru

import (
	"net/http"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/oauth-yandex-ru/routes"
)

func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", routes.Token)
	mux.HandleFunc("/", httpresponse.UpstreamDisabled)
	return mux
}
