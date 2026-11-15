package stages

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
	"github.com/fr33dman/yandex-station-picklock/pkg/station"
)

type remoteCommand func(context.Context, string) (string, error)

func rootADB(s *Session) remoteCommand {
	return func(ctx context.Context, script string) (string, error) { return s.Device.ADB(ctx, script, true) }
}

// Full root is checked through Telnet/SSH, not inferred from `su 0` in ADB.
const rootProof = `set -eu
test "$(id -u)" = 0
test "$(getprop ro.product.device)" = tulip-d1
test "$(/system/bin/busybox-smp awk '$1=="CapEff:" {print $2}' /proc/self/status)" = 0000001fffffffff
test "$(/system/bin/busybox-smp awk '$1=="CapBnd:" {print $2}' /proc/self/status)" = 0000001fffffffff
printf 'ROOT_OK\n'`

const bootPreflight = `set -eu
test "$(id -u)" = 0
test "$(getprop ro.product.device)" = tulip-d1
test "$(getprop ro.product.board)" = tulip
test "$(getprop ro.build.version.incremental)" = 20250703
test -b /dev/block/mmcblk0p6
test "$(cat /sys/class/block/mmcblk0p6/size)" = 32768
test -x /system/bin/busybox-smp
`

func remoteDirectory(ctx context.Context, s *Session) (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	name := "/data/local/tmp/picklock_" + hex.EncodeToString(token[:])
	_, err := s.Device.ADB(ctx, "umask 077; mkdir "+station.Quote(name)+" && chmod 711 "+station.Quote(name), false)
	return name, err
}

func pull(ctx context.Context, s *Session, remote string, limit int64) ([]byte, error) {
	f, err := os.CreateTemp(s.directory, "transfer-*")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		return nil, err
	}
	defer os.Remove(name)
	if err := s.Device.Pull(ctx, remote, name); err != nil {
		return nil, err
	}
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("device file exceeds its allowed size")
	}
	return os.ReadFile(name)
}

func readBoot(ctx context.Context, s *Session, run remoteCommand) ([]byte, error) {
	directory, err := remoteDirectory(ctx, s)
	if err != nil {
		return nil, err
	}
	remote, err := prepareSnapshot(ctx, s, directory)
	if err != nil {
		return nil, err
	}
	return snapshotBoot(ctx, s, run, remote)
}

func prepareSnapshot(ctx context.Context, s *Session, directory string) (string, error) {
	remote := directory + "/snapshot"
	// Shell owns the file; su without DAC_OVERRIDE must still be able to write it.
	if _, err := s.Device.ADB(ctx, "touch "+station.Quote(remote)+" && chmod 666 "+station.Quote(remote), false); err != nil {
		return "", err
	}
	return remote, nil
}

func snapshotBoot(ctx context.Context, s *Session, run remoteCommand, remote string) ([]byte, error) {
	output, err := run(ctx, bootPreflight+station.Busybox+" dd if="+station.BootBlock+" of="+station.Quote(remote)+" bs=1048576 && sync && "+station.Busybox+" sha256sum "+station.Quote(remote))
	if err != nil {
		return nil, fmt.Errorf("read boot (snapshot %s): %w", remote, err)
	}
	data, err := pull(ctx, s, remote, boot.Size)
	if err != nil {
		return nil, err
	}
	if len(data) != boot.Size || !hasDigest(output, boot.SHA256(data)) {
		return nil, errors.New("downloaded boot does not match the device snapshot")
	}
	// Keep remote snapshots available for recovery; never remove an unknown path.
	return data, nil
}

func hasDigest(output, expected string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 && fields[0] == expected {
			return true
		}
	}
	return false
}

// writeBoot separates cancellable preparation from the critical write/readback.
// Once dd starts, cancellation is deferred until the partition has been checked.
func writeBoot(ctx context.Context, s *Session, data []byte, currentSHA, name string, run remoteCommand) (result error) {
	stock, err := os.ReadFile(filepath.Join(s.directory, "boot-original.img"))
	if err != nil {
		return err
	}
	if err := boot.Verify(stock, boot.StockSHA256); err != nil {
		return fmt.Errorf("independent original backup: %w", err)
	}
	const lock = "/data/local/tmp/picklock_boot_write.lock"
	if _, err := s.Device.ADB(ctx, "umask 077; mkdir "+lock, false); err != nil {
		return fmt.Errorf("boot write lock is held by another or incomplete run: %w", err)
	}
	started, verified := false, false
	defer func() {
		// An unverified write keeps its lock so another run cannot overwrite evidence.
		if started && !verified {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, err := s.Device.ADB(cleanup, "rmdir "+lock, false)
		result = errors.Join(result, err)
	}()
	directory, err := remoteDirectory(ctx, s)
	if err != nil {
		return err
	}
	snapshot, err := prepareSnapshot(ctx, s, directory)
	if err != nil {
		return err
	}
	before, err := snapshotBoot(ctx, s, run, snapshot)
	if err != nil {
		return err
	}
	if boot.SHA256(before) != currentSHA {
		return errors.New("current boot changed since preparation; refusing to write")
	}
	backup, err := s.save("boot-before-"+name+".img", before)
	if err != nil {
		return err
	}
	target := boot.SHA256(data)
	if target != boot.RootSHA256 && target != boot.NoTelnetSHA256 {
		return errors.New("refusing an unrecognized boot candidate")
	}
	local := filepath.Join(s.directory, "boot-"+name+".img")
	stored, err := os.ReadFile(local)
	if err != nil || !bytes.Equal(stored, data) {
		return errors.New("prepared local boot is missing or changed")
	}
	remote := directory + "/candidate"
	if err := s.Device.Push(ctx, local, remote); err != nil {
		return err
	}
	if _, err := s.Device.ADB(ctx, "chmod 644 "+station.Quote(remote), false); err != nil {
		return err
	}
	output, err := run(ctx, station.Busybox+" sha256sum "+station.Quote(remote))
	if err != nil {
		return err
	}
	if !hasDigest(output, target) {
		return errors.New("uploaded boot SHA-256 mismatch")
	}
	// Reserve the readback before changing any byte of the partition.
	after, err := os.OpenFile(backup+".after", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer after.Close()
	confirmed, err := s.UI.Confirm(ctx, fmt.Sprintf("Записать boot %s? Текущий SHA-256: %s. Новый: %s. Бэкап: %s. После начала запись и проверка завершаются даже при отмене сценария. При ошибке нельзя перезагружать колонку.", name, currentSHA, target, backup))
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("boot write declined")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	critical, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	started = true
	script := bootPreflight + "test \"$(" + station.Busybox + " sha256sum " + station.BootBlock + " | " + station.Busybox + " cut -d ' ' -f1)\" = " + station.Quote(currentSHA) + "\n" +
		station.Busybox + " dd if=" + station.Quote(remote) + " of=" + station.BootBlock + " bs=1048576\nsync"
	if _, err := run(critical, script); err != nil {
		return fmt.Errorf("boot write failed; DO NOT REBOOT; backup %s, staging %s: %w", backup, directory, err)
	}
	// Reuse the already allocated full-size snapshot. Running out of /data space
	// after dd must not prevent the readback from being taken.
	readback, err := snapshotBoot(critical, s, run, snapshot)
	if err != nil {
		return fmt.Errorf("boot readback failed; DO NOT REBOOT: %w", err)
	}
	_, writeErr := after.Write(readback)
	if err := errors.Join(writeErr, after.Sync(), after.Close(), syncDirectory(s.directory)); err != nil {
		return fmt.Errorf("save readback; DO NOT REBOOT: %w", err)
	}
	if !bytes.Equal(readback, data) {
		return errors.New("boot readback differs from the candidate; DO NOT REBOOT")
	}
	s.writtenSHA256 = target
	verified = true
	_, err = s.Device.ADB(critical, "rm -f "+station.Quote(remote)+" "+station.Quote(snapshot)+" && rmdir "+station.Quote(directory), false)
	if err != nil {
		return fmt.Errorf("boot verified but staging cleanup failed: %w", err)
	}
	return nil
}
