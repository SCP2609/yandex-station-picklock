package stages

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fr33dman/yandex-station-picklock/artifacts"
	"github.com/fr33dman/yandex-station-picklock/pkg/boot"
)

func artifact(t *testing.T, name string) []byte {
	t.Helper()
	data, err := artifacts.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCheckpointBootProfiles(t *testing.T) {
	stock := artifact(t, artifacts.Station1StockBoot)
	reference := artifact(t, artifacts.Station1Boot)
	prepared, err := boot.Prepare(stock, reference)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prepared, reference) {
		t.Fatal("prepared boot differs from the independently verified checkpoint candidate")
	}
	packed := artifact(t, artifacts.Station1NoTelnetRamdisk)
	final, err := boot.DisableTelnet(prepared, packed)
	if err != nil {
		t.Fatal(err)
	}
	if boot.SHA256(final) != boot.NoTelnetSHA256 || !bytes.Equal(stock[14577664:], final[14577664:]) || !bytes.Equal(stock[2048:12632792], final[2048:12632792]) {
		t.Fatal("SSH-only boot changed kernel, tail or expected digest")
	}
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if bytes.Contains(raw, []byte("service rootshell ")) || !bytes.Contains(raw, []byte("service.adb.tcp.port=5555\n")) || !bytes.Contains(raw, []byte("ro.debuggable=1\n")) {
		t.Fatal("final ramdisk did not retain ADB while removing Telnet")
	}
	stock[4096] ^= 1
	if _, err := boot.Prepare(stock, reference); err == nil {
		t.Fatal("unsupported stock boot accepted")
	}
	packed[len(packed)-1] ^= 1
	if _, err := boot.DisableTelnet(reference, packed); err == nil {
		t.Fatal("modified ramdisk artifact accepted")
	}
}

func TestSSHProfileAndShellSyntax(t *testing.T) {
	files, err := sshFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["libc.musl-aarch64.so.1"], []byte("/dev/ssh/pw\x00")) || bytes.Contains(files["libc.musl-aarch64.so.1"], []byte("/etc/passwd\x00")) {
		t.Fatal("musl still depends on system account files")
	}
	for name, script := range map[string]string{"firewall": firewallScript, "dropbear": string(files["start.sh"])} {
		cmd := exec.Command("/bin/sh", "-n")
		cmd.Stdin = strings.NewReader(script)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s syntax: %v %s", name, err, output)
		}
	}
	if boot.SHA256(artifact(t, "ssh/station-1/rc.local.template")) != originalRCSHA256 {
		t.Fatal("stock rc.local fixture differs from the checked profile")
	}
}

func TestBackupsNeverOverwriteFilesOrSymlinks(t *testing.T) {
	s := &Session{directory: t.TempDir()}
	name, err := s.save("boot.img", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.save("boot.img", []byte("replacement")); err == nil {
		t.Fatal("backup was overwritten")
	}
	link := filepath.Join(s.directory, "link.img")
	if err := os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.save("link.img", []byte("replacement")); err == nil {
		t.Fatal("followed a backup symlink")
	}
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "original" {
		t.Fatalf("backup changed: %q %v", data, err)
	}
	info, err := os.Stat(name)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup permissions are not 0600")
	}
}
