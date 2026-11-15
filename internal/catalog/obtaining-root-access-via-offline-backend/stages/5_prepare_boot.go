package stages

import (
	"context"

	"github.com/fr33dman/yandex-station-picklock/artifacts"
	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
)

func PrepareBoot(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	reference, err := artifacts.ReadFile(artifacts.Station1Boot)
	if err != nil {
		return err
	}
	candidate, err := boot.Prepare(s.original, reference)
	if err != nil {
		return err
	}
	if _, err := s.save("boot-root.img", candidate); err != nil {
		return err
	}
	s.candidate = candidate
	return nil
}
