package stages

import (
	"context"
	"errors"

	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
)

func Reboot(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	if s.writtenSHA256 != boot.RootSHA256 {
		return errors.New("boot readback has not been verified; refusing to reboot")
	}
	confirmed, err := s.UI.Confirm(ctx, "Boot записан и проверен чтением обратно. Перезагрузить колонку?")
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("reboot declined")
	}
	return s.Device.Reboot(ctx)
}
