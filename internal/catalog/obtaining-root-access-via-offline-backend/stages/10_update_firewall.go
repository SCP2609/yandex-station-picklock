package stages

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
	"github.com/fr33dman/yandex-station-picklock/pkg/station"
)

const firewallScript = `#!/system/bin/sh
set -eu
IPT=/system/bin/iptables
CHAIN=PICKLOCK_ADB
if "$IPT" -N "$CHAIN" 2>/dev/null; then
    "$IPT" -A "$CHAIN" -s 192.168.0.0/16 -j ACCEPT
    "$IPT" -A "$CHAIN" -j DROP
else
    # Reject an unexpected existing chain instead of flushing active protection.
    expected=$(printf '%s\n' '-N PICKLOCK_ADB' '-A PICKLOCK_ADB -s 192.168.0.0/16 -j ACCEPT' '-A PICKLOCK_ADB -j DROP')
    test "$("$IPT" -S "$CHAIN")" = "$expected"
fi
"$IPT" -C INPUT -p tcp --dport 5555 -j "$CHAIN" 2>/dev/null || "$IPT" -I INPUT 1 -p tcp --dport 5555 -j "$CHAIN"
# An IPv6 listener must not bypass the IPv4 restriction.
if [ -x /system/bin/ip6tables ]; then
    /system/bin/ip6tables -C INPUT -p tcp --dport 5555 -j DROP 2>/dev/null || /system/bin/ip6tables -I INPUT 1 -p tcp --dport 5555 -j DROP
elif [ -r /proc/net/if_inet6 ] && [ -n "$(cat /proc/net/if_inet6)" ]; then
    echo 'IPv6 is active but ip6tables is unavailable' >&2
    exit 1
fi
`

// UpdateFirewall Stage
// Добавляем правило что к 5555 порту доступ только с 192.168.*.* адресов (то есть с локалки)
func UpdateFirewall(ctx context.Context) error {
	s, err := session(ctx)
	if err != nil {
		return err
	}
	if !s.sshVerified {
		return errors.New("SSH must be verified before restricting ADB")
	}
	if _, err := s.Device.SSH(ctx, rootProof); err != nil {
		return err
	}
	local, err := s.save("firewall.sh", []byte(firewallScript))
	if err != nil {
		return err
	}
	directory, err := remoteDirectory(ctx, s)
	if err != nil {
		return err
	}
	remote := directory + "/firewall.sh"
	if err := s.Device.Push(ctx, local, remote); err != nil {
		return err
	}
	// The only INPUT rule we add targets TCP/5555. SSH remains independently usable.
	if _, err := s.Device.SSH(ctx, "set -eu\ntest ! -e /data/dropbear/firewall.sh\ntest \"$("+station.Busybox+" sha256sum "+station.Quote(remote)+" | "+station.Busybox+" cut -d ' ' -f1)\" = "+station.Quote(boot.SHA256([]byte(firewallScript)))+"\n/system/bin/sh -n "+station.Quote(remote)+"\n"+station.Busybox+" cp "+station.Quote(remote)+" /data/dropbear/firewall.sh\nchmod 700 /data/dropbear/firewall.sh\nsync\n/system/bin/sh /data/dropbear/firewall.sh"); err != nil {
		return err
	}
	original, err := pull(ctx, s, "/system/etc/rc.local", 1<<20)
	if err != nil {
		return err
	}
	if bytes.Count(original, []byte(sshHook)) != 1 || strings.Contains(string(original), "/data/dropbear/firewall.sh") {
		return errors.New("unexpected startup configuration before firewall installation")
	}
	hook := "/system/bin/sh /data/dropbear/firewall.sh || true\n"
	if bytes.Count(original, []byte(rcAnchor)) != 1 {
		return errors.New("vendor startup anchor is missing")
	}
	updated := bytes.Replace(original, []byte(rcAnchor), []byte(rcAnchor+hook), 1)
	if err := installRCLocal(ctx, s, original, updated, "rc.local.before-firewall"); err != nil {
		return err
	}
	_, err = s.Device.SSH(ctx, rootProof+"\n/system/bin/iptables -C INPUT -p tcp --dport 5555 -j PICKLOCK_ADB")
	return err
}
