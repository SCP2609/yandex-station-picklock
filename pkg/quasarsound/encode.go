// Package quasarsound encodes the Station's local Wi-Fi pairing protocol.
package quasarsound

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const SampleRate = 16000

type Config struct {
	SSID     string
	Password string
	Hidden   bool
	Token    string
	// WiFiType follows the APK modes: wpa (default), wep, open or none (auth only).
	WiFiType string
}

// Encode emits mono PCM16 WAV. Keep each tone at exactly 40 ms, without a window.
func Encode(config Config) ([]byte, error) {
	payload, err := Payload(config)
	if err != nil {
		return nil, err
	}
	symbols := encodeSymbols(payload)
	tones := []int{6192, 6192, 5120, 5120, 1024, 1024}
	for _, symbol := range symbols {
		tones = append(tones, 1024+int(symbol)*256)
	}
	tones = append(tones, 5632, 5632, 5632, 5632, 5632, 5632, 5632, 5632)
	const samplesPerTone = SampleRate * 40 / 1000
	dataSize := len(tones) * samplesPerTone * 2
	wav := make([]byte, 44+dataSize)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(36+dataSize))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], SampleRate)
	binary.LittleEndian.PutUint32(wav[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(dataSize))
	position := 44
	for _, frequency := range tones {
		for i := 0; i < samplesPerTone; i++ {
			// The JS reference rounds through Float32 before quantizing to PCM16.
			sample := float32(math.Sin(2*math.Pi*float64(frequency)*float64(i)/SampleRate) * 0.95)
			binary.LittleEndian.PutUint16(wav[position:], uint16(int16(float64(sample)*32767)))
			position += 2
		}
	}
	return wav, nil
}

func Payload(config Config) ([]byte, error) {
	security := strings.ToLower(config.WiFiType)
	if security == "" {
		security = "wpa"
	}
	if security != "wpa" && security != "wep" && security != "open" && security != "none" {
		return nil, errors.New("unsupported pairing Wi-Fi type")
	}
	if config.Token == "" || (security != "none" && (config.SSID == "" || config.Password == "")) {
		return nil, errors.New("network pairing requires SSID and password; every mode requires a token")
	}
	for _, field := range []string{config.SSID, config.Password, config.Token} {
		if !utf8.ValidString(field) || len(field) > 255 {
			return nil, errors.New("pairing fields must contain at most 255 valid UTF-8 bytes")
		}
	}
	var payload []byte
	appendString := func(value string) { payload = append(payload, byte(len(value))); payload = append(payload, value...) }
	switch {
	case security == "none":
		payload = append(payload, 4)
	case config.Hidden:
		mode := byte(2)
		if security == "wep" {
			mode = 1
		} else if security == "open" {
			mode = 0
		}
		payload = append(payload, mode)
		appendString(config.SSID)
		appendString(config.Password)
	default:
		payload = append(payload, 3)
		// Java hashes UTF-16 code units and uses a signed remainder.
		var hash int32
		for _, unit := range utf16.Encode([]rune(config.SSID)) {
			hash = hash*31 + int32(unit)
		}
		hash %= 32767
		payload = append(payload, byte(hash), byte(hash>>8))
		appendString(config.Password)
	}
	appendString(config.Token)
	checksum := crc32.ChecksumIEEE(payload)
	// The protocol transmits only the low 16 bits of the IEEE CRC32, little endian.
	return append(payload, byte(checksum), byte(checksum>>8)), nil
}

func multiply(a, b byte) byte {
	var result byte
	for b != 0 {
		if b&1 != 0 {
			result ^= a
		}
		b >>= 1
		a <<= 1
		if a&16 != 0 {
			a ^= 0x13
		}
	}
	return result
}

func encodeSymbols(payload []byte) []byte {
	var symbols []byte
	for _, value := range payload {
		symbols = append(symbols, value>>4, value&15)
	}
	// RS(15,11) over GF(16), primitive polynomial x^4+x+1, roots alpha^1..4.
	generator := []byte{1}
	root := byte(1)
	for range 4 {
		root = multiply(root, 2)
		next := make([]byte, len(generator)+1)
		for i, value := range generator {
			next[i] ^= value
			next[i+1] ^= multiply(value, root)
		}
		generator = next
	}
	var encoded []byte
	for offset := 0; offset < len(symbols); offset += 11 {
		block := symbols[offset:min(offset+11, len(symbols))]
		var parity [4]byte
		for _, value := range block {
			factor := value ^ parity[0]
			copy(parity[:], parity[1:])
			parity[3] = 0
			for i := range parity {
				parity[i] ^= multiply(generator[i+1], factor)
			}
		}
		encoded = append(encoded, block...)
		encoded = append(encoded, parity[:]...)
	}
	return encoded
}
