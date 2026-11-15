package stages

import (
	"bytes"
	"context"
	"errors"

	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
)

// AddDropbear Stage
// Добавляем Dropbear в автозагрузку
// Добавляем ssh ключ в доверенный на колонку
func AddDropbear(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	if !s.rootVerified {
		return errors.New("full root must be verified before SSH installation")
	}
	if err := installSSH(ctx, s); err != nil {
		return err
	}
	// Obtain the host key through the already checked root channel, then pin it.
	output, err := s.Device.Telnet(ctx, "/data/dropbear/ld-musl-aarch64.so.1 --library-path /data/dropbear /data/dropbear/dropbearkey -y -f /data/dropbear/hostkey")
	if err != nil {
		return err
	}
	key, err := sshHostKey(output)
	if err != nil {
		return err
	}
	if err := s.Device.TrustHostKey(knownHostsPath(s), key); err != nil {
		return err
	}
	if _, err := s.Device.SSH(ctx, rootProof); err != nil {
		return err
	}
	original, err := pull(ctx, s, "/system/etc/rc.local", 1<<20)
	if err != nil {
		return err
	}
	if boot.SHA256(original) != originalRCSHA256 || bytes.Count(original, []byte(rcAnchor)) != 1 || bytes.Contains(original, []byte(sshHook)) {
		return errors.New("unexpected rc.local; refusing to change startup")
	}
	updated := bytes.Replace(original, []byte(rcAnchor), []byte(sshHook+rcAnchor), 1)
	if err := installRCLocal(ctx, s, original, updated, "rc.local.original"); err != nil {
		return err
	}
	if _, err := s.Device.SSH(ctx, rootProof); err != nil {
		return err
	}
	s.sshVerified = true
	return nil
}
