package stages

import (
	"context"

	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
)

func WriteBoot(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	if err := boot.Verify(s.candidate, boot.RootSHA256); err != nil {
		return err
	}
	return writeBoot(ctx, s, s.candidate, boot.StockSHA256, "root", rootADB(s))
}
