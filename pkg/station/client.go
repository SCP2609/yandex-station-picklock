// Package station implements the device's ADB, temporary Telnet and key-only SSH transports.
package station

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Busybox = "/system/bin/busybox-smp"
const BootBlock = "/dev/block/mmcblk0p6"

type Client struct {
	adb        string
	identity   string
	knownHosts string
	address    netip.Addr
}

func NewClient(adb, identity string) (*Client, error) {
	if adb == "" {
		adb = "adb"
	}
	program, err := exec.LookPath(adb)
	if err != nil {
		return nil, fmt.Errorf("ADB executable: %w", err)
	}
	identity, err = filepath.Abs(identity)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(identity)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("SSH identity must be an existing regular file")
	}
	return &Client{adb: program, identity: identity}, nil
}

func (c *Client) Connect(ctx context.Context, address netip.Addr) error {
	if !address.Is4() || !address.IsPrivate() {
		return errors.New("station must have a private IPv4 address")
	}
	c.address = address
	if err := c.Probe(ctx, 5555); err != nil {
		return err
	}
	if _, err := command(ctx, c.adb, "connect", c.serial()); err != nil {
		return err
	}
	output, err := command(ctx, c.adb, "-s", c.serial(), "get-state")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(output)) != "device" {
		return errors.New("ADB did not confirm the selected device")
	}
	return nil
}

func (c *Client) serial() string { return net.JoinHostPort(c.address.String(), "5555") }

// Android 6 shell may return a local success for a failed remote command.
// A random final marker carries the actual exit status across that boundary.
func (c *Client) ADB(ctx context.Context, script string, root bool) (string, error) {
	if !c.address.IsValid() {
		return "", errors.New("station is not connected")
	}
	wrapped := "/system/bin/sh -c " + Quote(script)
	if root {
		wrapped = "su 0 " + wrapped
	}
	marker := newMarker()
	wrapped += "; picklock_rc=$?; printf '\\n" + marker + ":%s\\n' \"$picklock_rc\""
	output, err := command(ctx, c.adb, "-s", c.serial(), "shell", wrapped)
	if err != nil {
		return "", err
	}
	return parseResult(string(output), marker)
}

func (c *Client) Push(ctx context.Context, local, remote string) error {
	_, err := command(ctx, c.adb, "-s", c.serial(), "push", local, remote)
	return err
}

func (c *Client) Pull(ctx context.Context, remote, local string) error {
	_, err := command(ctx, c.adb, "-s", c.serial(), "pull", remote, local)
	return err
}

func (c *Client) Reboot(ctx context.Context) error {
	_, err := command(ctx, c.adb, "-s", c.serial(), "reboot")
	return err
}

func (c *Client) Probe(ctx context.Context, port uint16) error {
	if !c.address.IsValid() {
		return errors.New("station address is not set")
	}
	connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(c.address.String(), strconv.Itoa(int(port))))
	if err != nil {
		return err
	}
	return connection.Close()
}

func (c *Client) TrustHostKey(filename, key string) error {
	if err := ValidatePublicKey(key); err != nil {
		return err
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(f, c.address.String()+" "+strings.Join(strings.Fields(key)[:2], " "))
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	c.knownHosts = filename
	return nil
}

func (c *Client) SSH(ctx context.Context, script string) (string, error) {
	if c.knownHosts == "" {
		return "", errors.New("station SSH host key has not been pinned")
	}
	output, err := command(ctx, "ssh", "-F", "/dev/null", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+strconv.Quote(strings.ReplaceAll(c.knownHosts, "%", "%%")),
		"-o", "ConnectTimeout=5", "-i", c.identity, "root@"+c.address.String(), "/system/bin/sh -c "+Quote(script))
	return string(output), err
}

// ValidatePublicKey accepts one complete Ed25519 key, with an optional comment.
func ValidatePublicKey(key string) error {
	key = strings.TrimSuffix(strings.TrimSuffix(key, "\n"), "\r")
	if strings.ContainsAny(key, "\r\n\x00") || len(key) > 8192 {
		return errors.New("expected one Ed25519 public key")
	}
	fields := strings.Fields(key)
	if len(fields) < 2 || fields[0] != "ssh-ed25519" {
		return errors.New("expected an Ed25519 public key")
	}
	wire, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(wire) != 51 || binary.BigEndian.Uint32(wire[:4]) != 11 || string(wire[4:15]) != "ssh-ed25519" || binary.BigEndian.Uint32(wire[15:19]) != 32 {
		return errors.New("invalid Ed25519 public key encoding")
	}
	return nil
}

func Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var output commandOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if output.truncated {
		err = errors.Join(err, errors.New("command output exceeded 1 MiB"))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(name), errors.Join(err, ctx.Err()), strings.TrimSpace(string(output.data)))
	}
	return output.data, nil
}

type commandOutput struct {
	data      []byte
	truncated bool
}

func (o *commandOutput) Write(data []byte) (int, error) {
	const limit = 1 << 20
	remaining := limit - len(o.data)
	o.data = append(o.data, data[:min(len(data), remaining)]...)
	if len(data) > remaining {
		o.truncated = true
	}
	// Keep draining so a noisy subprocess cannot block on a full stdout pipe.
	return len(data), nil
}

func newMarker() string {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(err)
	}
	return "PICKLOCK_" + hex.EncodeToString(token[:])
}

func parseResult(output, marker string) (string, error) {
	clean := strings.ReplaceAll(output, "\r", "")
	index := strings.LastIndex(clean, "\n"+marker+":")
	if index < 0 {
		return "", errors.New("remote command did not confirm its exit status")
	}
	status, _, complete := strings.Cut(clean[index+len(marker)+2:], "\n")
	if !complete {
		return "", errors.New("remote exit marker is incomplete")
	}
	status = strings.TrimSpace(status)
	if status != "0" {
		return "", fmt.Errorf("remote command failed (status %s): %s", status, strings.TrimSpace(clean[:index]))
	}
	return strings.TrimSpace(clean[:index]), nil
}

func (c *Client) Telnet(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(c.address.String(), "2323"))
	if err != nil {
		return "", err
	}
	defer connection.Close()
	output, runErr := runTelnet(ctx, connection, script)
	return output, errors.Join(runErr, ctx.Err())
}

const (
	telnetData = iota
	telnetCommand
	telnetOption
	telnetSubnegotiation
	telnetSubcommand
)

const (
	iac               byte = 255
	will              byte = 251
	wont              byte = 252
	do                byte = 253
	dont              byte = 254
	subnegotiation    byte = 250
	endSubnegotiation byte = 240
)

func runTelnet(ctx context.Context, connection net.Conn, script string) (string, error) {
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = connection.SetDeadline(deadline)
	start, finish := newMarker(), newMarker()
	// Base64 keeps shell echo from imitating either marker and quotes script data.
	decoded := []byte("printf '\\n" + start + "\\n'\n" + script)
	payload := base64.StdEncoding.EncodeToString(decoded)
	directory := "/data/local/tmp/." + newMarker()
	remote := directory + "/encoded"
	var commands strings.Builder
	fmt.Fprintf(&commands, "umask 077; mkdir -m 700 %s && (set -C; : > %s) || exit\n", Quote(directory), Quote(remote))
	// Canonical PTYs can truncate a line above 4096 bytes. Deliver bounded lines,
	// then verify the decoded script before executing even its first command.
	for offset := 0; offset < len(payload); offset += 768 {
		fmt.Fprintf(&commands, "printf %%s %s >> %s || exit\n", Quote(payload[offset:min(offset+768, len(payload))]), Quote(remote))
	}
	digest := sha256.Sum256(decoded)
	fmt.Fprintf(&commands, "%s base64 -d %s > %s && test \"$(%s sha256sum %s | %s cut -d ' ' -f1)\" = %s && /system/bin/sh %s; picklock_rc=$?; %s rm -f %s %s; rmdir %s; printf '\\n%s:%%s\\n' \"$picklock_rc\"\n",
		Busybox, Quote(remote), Quote(remote+".sh"), Busybox, Quote(remote+".sh"), Busybox, Quote(hex.EncodeToString(digest[:])), Quote(remote+".sh"), Busybox, Quote(remote), Quote(remote+".sh"), Quote(directory), finish)
	if _, err := ioWrite(connection, []byte(commands.String())); err != nil {
		return "", err
	}
	var output bytes.Buffer
	state, verb := telnetData, byte(0)
	buffer := make([]byte, 4096)
	for output.Len() < 1<<20 {
		n, readErr := connection.Read(buffer)
		for _, value := range buffer[:n] {
			switch state {
			case telnetData:
				if value == iac {
					state = telnetCommand
				} else {
					output.WriteByte(value)
				}
			case telnetCommand:
				switch value {
				case iac:
					output.WriteByte(iac)
					state = telnetData
				case will, wont, do, dont:
					verb = value
					state = telnetOption
				case subnegotiation:
					state = telnetSubnegotiation
				default:
					state = telnetData
				}
			case telnetOption:
				if verb == will || verb == do {
					reply := dont
					if verb == do {
						reply = wont
					}
					if _, err := ioWrite(connection, []byte{iac, reply, value}); err != nil {
						return "", err
					}
				}
				state = telnetData
			case telnetSubnegotiation:
				if value == iac {
					state = telnetSubcommand
				}
			case telnetSubcommand:
				if value == endSubnegotiation {
					state = telnetData
				} else {
					state = telnetSubnegotiation
				}
			}
		}
		clean := strings.ReplaceAll(output.String(), "\r", "")
		begin := strings.Index(clean, "\n"+start+"\n")
		if begin >= 0 {
			body := clean[begin+len(start)+2:]
			end := strings.LastIndex(body, "\n"+finish+":")
			if end >= 0 && strings.Contains(body[end+len(finish)+2:], "\n") {
				return parseResult(body, finish)
			}
		}
		if readErr != nil {
			return "", fmt.Errorf("Telnet result not confirmed: %w", readErr)
		}
	}
	return "", errors.New("Telnet output exceeded 1 MiB")
}

// net.Conn.Write may be short; command delivery must be all-or-error.
func ioWrite(connection net.Conn, data []byte) (int, error) {
	written := 0
	for written < len(data) {
		n, err := connection.Write(data[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, errors.New("short network write")
		}
	}
	return written, nil
}
