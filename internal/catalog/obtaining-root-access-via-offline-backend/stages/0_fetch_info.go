package stages

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/fr33dman/yandex-station-picklock/pkg/alice"
)

func FetchInfo(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	model, err := s.UI.Ask(ctx, Question{Text: "Модель станции (поддерживается station-1)", Default: alice.YandexStation1})
	if err != nil {
		return err
	}
	if model == "" {
		model = alice.YandexStation1
	}
	if model != alice.YandexStation1 {
		return errors.New("offline root is supported only for station-1")
	}
	ssid, err := s.UI.Ask(ctx, Question{Text: "Имя изолированной Wi-Fi сети (SSID)"})
	if err != nil {
		return err
	}
	password, err := s.UI.Ask(ctx, Question{Text: "Пароль WPA/WPA2 сети", Secret: true})
	if err != nil {
		return err
	}
	hidden, err := s.UI.Confirm(ctx, "Сеть Wi-Fi скрытая?")
	if err != nil {
		return err
	}
	if !utf8.ValidString(ssid) || len(ssid) < 1 || len(ssid) > 32 {
		return errors.New("SSID must contain 1–32 UTF-8 bytes")
	}
	if !utf8.ValidString(password) || len(password) < 8 || len(password) > 63 {
		return errors.New("WPA password must contain 8–63 UTF-8 bytes")
	}
	// Do not include credentials in errors, status snapshots or logs.
	s.model, s.ssid, s.password, s.hidden = model, ssid, password, hidden
	return nil
}
