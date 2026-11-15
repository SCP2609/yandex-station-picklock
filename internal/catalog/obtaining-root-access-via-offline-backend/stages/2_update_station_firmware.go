package stages

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/fr33dman/yandex-station-picklock/artifacts"
	"github.com/fr33dman/yandex-station-picklock/pkg/alice"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/firmware"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

func UpdateStationFirmware(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	if s.model != alice.YandexStation1 {
		return errors.New("station-1 must be selected before OTA")
	}
	if err := s.Backend.Replace(ctx, nil); err != nil {
		return err
	}
	// TODO: compare the version reported by the station with the selected profile.
	// Eligibility still requires the exact factory version in /check_updates.
	image, err := firmware.NewEmbedded(artifacts.FS(), artifacts.Station1Firmware, artifacts.Station1FirmwareSHA256)
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(s.config.PublicURL, "/") + "/ota/" + path.Base(artifacts.Station1Firmware)
	update, err := ota.NewUpdate(ota.Release{
		SourceVersion: artifacts.Station1FactoryVersion, Platform: "yandexstation",
		Version: artifacts.Station1FirmwareVersion, DownloadURL: url, Critical: true, UpdateID: "82789",
	}, image)
	if err != nil {
		return err
	}
	for {
		if err := s.Backend.Replace(ctx, update); err != nil {
			return err
		}
		if err := s.Backend.WaitDownload(ctx); err != nil {
			return err
		}
		confirmed, err := s.UI.Confirm(ctx, "Колонка сообщила, что обновление скачано?")
		if err != nil {
			return err
		}
		// Keep sync available for ADB, while preventing another OTA attempt.
		if err := s.Backend.Replace(ctx, nil); err != nil {
			return err
		}
		if confirmed {
			return nil
		}
		// A fresh backend has a fresh range tracker; replaying old bytes is not completion.
	}
}
