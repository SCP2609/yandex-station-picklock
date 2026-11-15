package stages

import (
	"context"
	"fmt"

	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
)

func RetrieveBoot(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	image, err := readBoot(ctx, s, rootADB(s))
	if err != nil {
		return err
	}
	backup, err := s.save("boot-original.img", image)
	if err != nil {
		return err
	}
	if err := boot.Verify(image, boot.StockSHA256); err != nil {
		return fmt.Errorf("unsupported boot; snapshot retained at %s: %w", backup, err)
	}
	s.original = image
	return nil
}
