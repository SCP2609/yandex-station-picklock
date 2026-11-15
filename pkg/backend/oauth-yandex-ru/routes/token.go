package routes

import (
	"net/http"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/internal/httpresponse"
)

func Token(w http.ResponseWriter, r *http.Request) {
	// These tokens are only for the offline backend, never for upstream OAuth.
	httpresponse.JSON(w, r, http.StatusOK, struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}{"local-offline-access", "local-offline-refresh", "bearer", 31536000})
}
