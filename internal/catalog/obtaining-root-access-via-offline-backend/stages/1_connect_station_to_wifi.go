package stages

import (
	"context"

	"github.com/fr33dman/yandex-station-picklock/pkg/quasarsound"
)

func ConnectStationToWifi(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	wav, err := quasarsound.Encode(quasarsound.Config{SSID: s.ssid, Password: s.password, Hidden: s.hidden, Token: "offline"})
	if err != nil {
		return err
	}
	s.password = ""
	defer clear(wav)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.UI.Play(ctx, wav); err != nil {
			return err
		}
		connected, err := s.UI.Confirm(ctx, "Колонка подключилась к изолированной Wi-Fi сети?")
		if err != nil {
			return err
		}
		if connected {
			return nil
		}
	}
}
