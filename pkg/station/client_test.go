package station

import (
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"testing"
)

func publicKey() string {
	wire := make([]byte, 51)
	binary.BigEndian.PutUint32(wire[:4], 11)
	copy(wire[4:], "ssh-ed25519")
	binary.BigEndian.PutUint32(wire[15:19], 32)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire) + " test"
}

func TestPublicKeysAndRemoteResults(t *testing.T) {
	if err := ValidatePublicKey(publicKey() + "\n"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "ssh-ed25519 !!!", publicKey() + "\n" + publicKey(), "ssh-rsa invalid", publicKey() + "\x00"} {
		if err := ValidatePublicKey(key); err == nil {
			t.Fatal("accepted malformed key")
		}
	}
	output, err := parseResult("hello\r\nMARK:0\r\n# ", "MARK")
	if err != nil || output != "hello" {
		t.Fatalf("Telnet prompt after marker: %q %v", output, err)
	}
	for _, raw := range []string{"hello", "hello\nMARK:0", "hello\nMARK:1\n", "hello\nMARK:0 garbage\n"} {
		if _, err := parseResult(raw, "MARK"); err == nil {
			t.Fatalf("accepted unconfirmed result: %q", raw)
		}
	}
}

func TestShellQuoting(t *testing.T) {
	value := "a'b;$(printf unexpected)"
	output, err := exec.Command("/bin/sh", "-c", "printf %s "+Quote(value)).Output()
	if err != nil || string(output) != value {
		t.Fatalf("shell interpreted quoted input: %q %v", output, err)
	}
}
