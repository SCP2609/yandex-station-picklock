package stages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fr33dman/yandex-station-picklock/artifacts"
	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
	"github.com/fr33dman/yandex-station-picklock/pkg/station"
)

const sshHook = "# Key-only SSH\n[ ! -x /data/dropbear/start.sh ] || /system/bin/sh /data/dropbear/start.sh || true\n"
const rcAnchor = "/system/vendor/quasar/onboot.sh\n"
const originalRCSHA256 = "fa89892f809079e7cc7701497a16889d035c6564f717a185ca0bc2cc208b3c91"

func sshFiles() (map[string][]byte, error) {
	expected := map[string]string{
		"dropbear":               "ee368311c8e4e9b99dc9a86644030cc039ef31befe8ba0a5be0251371b53eeb8",
		"dropbearkey":            "5f1c79dfc4a25994c66eaa9e326b9d8f47819c60ffcc507f4a825d3193faed68",
		"ld-musl-aarch64.so.1":   "4892d92d4c0dc5a772c2854f52cae3967aaaaa68f62813944146101b02ea74a6",
		"libc.musl-aarch64.so.1": "4892d92d4c0dc5a772c2854f52cae3967aaaaa68f62813944146101b02ea74a6",
		"libz.so.1":              "6e43ace7b43f3ebc9683dfdd0438b604dadc2a5743e3cd8a0ccee44ea7e7de2f",
		"start.sh":               "3e163b39dd1ad97d44dc6985ffab726b80a206ab565c4207d09050b03e2cc958",
	}
	files := make(map[string][]byte)
	for name, digest := range expected {
		data, err := artifacts.ReadFile("ssh/station-1/" + name)
		if err != nil {
			return nil, err
		}
		if boot.SHA256(data) != digest {
			return nil, fmt.Errorf("unexpected SSH artifact: %s", name)
		}
		if name == "ld-musl-aarch64.so.1" || name == "libc.musl-aarch64.so.1" {
			// Equal-length substitutions keep ELF offsets intact. System account
			// files remain unchanged; the private runtime account lives in /dev/ssh.
			for _, replacement := range [][2]string{{"/etc/passwd\x00", "/dev/ssh/pw\x00"}, {"/etc/group\x00", "/dev/ssh/g\x00"}, {"/etc/shadow\x00", "/dev/ssh/sp\x00"}, {"/etc/shells\x00", "/dev/ssh/sh\x00"}} {
				old, new := []byte(replacement[0]), []byte(replacement[1])
				if len(old) != len(new) || bytes.Count(data, old) != 1 {
					return nil, errors.New("unexpected musl account path layout")
				}
				data = bytes.Replace(data, old, new, 1)
			}
			if boot.SHA256(data) != "da29129dc11494761f687fd7a626f4d5957eb9d0f5488122169d91e871716253" {
				return nil, errors.New("patched musl checksum mismatch")
			}
		}
		files[name] = data
	}
	return files, nil
}

func installSSH(ctx context.Context, s *Session) error {
	files, err := sshFiles()
	if err != nil {
		return err
	}
	files["authorized_keys"] = []byte(strings.TrimSpace(s.publicKey) + "\n")
	directory, err := remoteDirectory(ctx, s)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var script strings.Builder
	script.WriteString("set -eu\ntest \"$(id -u)\" = 0\ntest \"$(getprop ro.product.device)\" = tulip-d1\ntest ! -e /data/dropbear\numask 077\n")
	for _, name := range names {
		local, err := s.save("ssh-"+name, files[name])
		if err != nil {
			return err
		}
		remote := directory + "/" + name
		if err := s.Device.Push(ctx, local, remote); err != nil {
			return err
		}
		fmt.Fprintf(&script, "test \"$(%s sha256sum %s | %s cut -d ' ' -f1)\" = %s\n", station.Busybox, station.Quote(remote), station.Busybox, station.Quote(boot.SHA256(files[name])))
	}
	// Use mkdir as an exclusive install lock; never overwrite an existing setup.
	staging := "/data/dropbear.install"
	script.WriteString("test ! -e " + staging + "\nmkdir -m 700 " + staging + "\nmkdir -m 700 " + staging + "/.ssh\n")
	for _, name := range names {
		target := staging + "/" + name
		mode := "700"
		if name == "authorized_keys" {
			target = staging + "/.ssh/authorized_keys"
			mode = "600"
		}
		fmt.Fprintf(&script, "%s cp %s %s\nchmod %s %s\n", station.Busybox, station.Quote(directory+"/"+name), station.Quote(target), mode, station.Quote(target))
	}
	fmt.Fprintf(&script, "%s/ld-musl-aarch64.so.1 --library-path %s %s/dropbearkey -t ed25519 -f %s/hostkey\n", staging, staging, staging, staging)
	script.WriteString("chmod 600 " + staging + "/hostkey\n/system/bin/sh -n " + staging + "/start.sh\ntest ! -e /data/dropbear\n" + station.Busybox + " mv -T " + staging + " /data/dropbear\nsync\n/system/bin/sh /data/dropbear/start.sh\n")
	if _, err := s.Device.Telnet(ctx, script.String()); err != nil {
		return fmt.Errorf("install key-only SSH (staging %s): %w", directory, err)
	}
	return nil
}

func sshHostKey(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ssh-ed25519 ") {
			if err := station.ValidatePublicKey(line); err != nil {
				return "", err
			}
			return line, nil
		}
	}
	return "", errors.New("Dropbear did not return an Ed25519 host key")
}

func installRCLocal(ctx context.Context, s *Session, old, new []byte, backupName string) error {
	if bytes.Equal(old, new) {
		return errors.New("rc.local update has no changes")
	}
	if _, err := s.save(backupName, old); err != nil {
		return err
	}
	local, err := s.save(backupName+".new", new)
	if err != nil {
		return err
	}
	directory, err := remoteDirectory(ctx, s)
	if err != nil {
		return err
	}
	remote := directory + "/rc.local"
	if err := s.Device.Push(ctx, local, remote); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	critical, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	backup := "/data/dropbear/" + backupName
	// A trap restores the original read-only mount even when a check fails.
	script := fmt.Sprintf(`set -eu
BB=%s
RC=/system/etc/rc.local
SRC=%s
BACKUP=%s
TMP=/system/etc/.picklock-rc-$$
test -f "$RC" && test ! -L "$RC"
test ! -e "$BACKUP" && test ! -e "$TMP"
test "$("$BB" sha256sum "$RC" | "$BB" cut -d ' ' -f1)" = %s
test "$("$BB" sha256sum "$SRC" | "$BB" cut -d ' ' -f1)" = %s
/system/bin/sh -n "$SRC"
opts=$("$BB" awk '$2=="/system" {print $4;exit}' /proc/mounts)
case ",$opts," in *,ro,*) :;; *) exit 1;; esac
"$BB" cp -p "$RC" "$BACKUP"
chmod 600 "$BACKUP"
test "$("$BB" sha256sum "$BACKUP" | "$BB" cut -d ' ' -f1)" = %s
remounted=0
cleanup() {
    result=$?
    trap - EXIT
    if [ "$remounted" = 1 ]; then
        "$BB" rm -f "$TMP"
        /system/bin/mount -o ro,remount /system || result=1
    fi
    exit "$result"
}
trap cleanup EXIT
/system/bin/mount -o rw,remount /system
remounted=1
"$BB" cp "$SRC" "$TMP"
chmod 755 "$TMP"
chown 0:0 "$TMP"
/system/bin/sh -n "$TMP"
"$BB" mv "$TMP" "$RC"
/system/bin/restorecon "$RC"
sync
test "$("$BB" sha256sum "$RC" | "$BB" cut -d ' ' -f1)" = %s
`, station.Busybox, station.Quote(remote), station.Quote(backup), station.Quote(boot.SHA256(old)), station.Quote(boot.SHA256(new)), station.Quote(boot.SHA256(old)), station.Quote(boot.SHA256(new)))
	if _, err := s.Device.SSH(critical, script); err != nil {
		return fmt.Errorf("update rc.local; backups retained in %s: %w", s.directory, err)
	}
	stored, err := pull(critical, s, "/system/etc/rc.local", 1<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(stored, new) {
		return errors.New("rc.local readback mismatch")
	}
	return nil
}

func knownHostsPath(s *Session) string { return filepath.Join(s.directory, "known_hosts") }
