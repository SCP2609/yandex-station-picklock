package station

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTelnetChunkingNegotiationAndPrompt(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	script := strings.Repeat("# a long script line\n", 1000) + "printf 'ok\\n'\n"
	peerResult := make(chan error, 1)
	go func() {
		peerResult <- func() error {
			scanner := bufio.NewScanner(server)
			var encoded, echo strings.Builder
			chunkPattern := regexp.MustCompile(`^printf %s '([^']*)' >>`)
			markerPattern := regexp.MustCompile(`PICKLOCK_[0-9a-f]{32}`)
			var finish string
			for scanner.Scan() {
				line := scanner.Text()
				if len(line) > 2048 {
					return errors.New("a line exceeds the PTY safety limit")
				}
				echo.WriteString(line + "\r\n")
				if chunk := chunkPattern.FindStringSubmatch(line); chunk != nil {
					encoded.WriteString(chunk[1])
				}
				if strings.Contains(line, "picklock_rc=$?") {
					markers := markerPattern.FindAllString(line, -1)
					finish = markers[len(markers)-1]
					break
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded.String())
			if err != nil {
				return err
			}
			if !strings.HasSuffix(string(decoded), script) {
				return errors.New("long script was truncated or changed")
			}
			digest := sha256.Sum256(decoded)
			if !strings.Contains(echo.String(), hex.EncodeToString(digest[:])) {
				return errors.New("decoded script is not checked before execution")
			}
			start := markerPattern.FindString(string(decoded))
			if start == "" || finish == "" {
				return errors.New("missing framing markers")
			}
			// Split an IAC negotiation across reads and verify refusal of WILL.
			if _, err := server.Write([]byte{iac}); err != nil {
				return err
			}
			if _, err := server.Write([]byte{will, 1}); err != nil {
				return err
			}
			reply := make([]byte, 3)
			if _, err := io.ReadFull(server, reply); err != nil {
				return err
			}
			if !bytes.Equal(reply, []byte{iac, dont, 1}) {
				return fmt.Errorf("negotiation reply: %v", reply)
			}
			if _, err := ioWrite(server, []byte(echo.String())); err != nil {
				return err
			}
			_, err = ioWrite(server, []byte("\r\n"+start+"\r\nok\r\n"+finish+":0\r\n# "))
			return err
		}()
	}()
	output, err := runTelnet(ctx, client, script)
	if err != nil || output != "ok" {
		t.Fatalf("Telnet command result: %q %v", output, err)
	}
	if err := <-peerResult; err != nil {
		t.Fatal(err)
	}
}
