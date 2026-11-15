package quasarsound

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// These vectors were produced by the user's JS encoder, not this implementation.
func TestJavaScriptCompatibility(t *testing.T) {
	for _, tc := range []struct {
		config       Config
		payload, wav string
	}{
		{Config{SSID: "Test WiFi", Password: "password123", Token: "local-offline"}, "032f510b70617373776f72643132330d6c6f63616c2d6f66666c696e653cee", "8279f9f4717ed5c6460eec3c26949b2a196707168cb0a5889ea012b81bfae0bb"},
		{Config{SSID: "Сеть📡", Password: "пароль123", Token: "local-offline"}, "036f060fd0bfd0b0d180d0bed0bbd18c3132330d6c6f63616c2d6f66666c696e65b18e", "700262f334e9f5d7647f343294547b0e5ae86ef032ea3264a152e90d97c6d171"},
		{Config{SSID: "zzzzzzzz", Password: "password123", Token: "local-offline", Hidden: true}, "02087a7a7a7a7a7a7a7a0b70617373776f72643132330d6c6f63616c2d6f66666c696e65abb2", "69b763be4a3c6c4ea7d0b386244c86edb88cbcba1a6162e624b39fb3a1f6fada"},
	} {
		t.Run(tc.config.SSID, func(t *testing.T) {
			payload, err := Payload(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(payload) != tc.payload {
				t.Fatalf("protocol payload: %x", payload)
			}
			wav, err := Encode(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(wav)
			if hex.EncodeToString(digest[:]) != tc.wav {
				t.Fatalf("WAV differs from JS: %x", digest)
			}
		})
	}
}

func TestInvalidPairingFields(t *testing.T) {
	for _, config := range []Config{{}, {SSID: "ssid", Password: "password"}, {SSID: string([]byte{255}), Password: "password", Token: "token"}} {
		if _, err := Encode(config); err == nil {
			t.Fatal("invalid pairing fields accepted")
		}
	}
}

func TestOtherAPKPairingModes(t *testing.T) {
	for _, tc := range []struct{ security, payload string }{
		{"wep", "0104737369640870617373776f726405746f6b656edf23"},
		{"open", "0004737369640870617373776f726405746f6b656e41a0"},
		{"none", "0405746f6b656ef83c"},
	} {
		payload, err := Payload(Config{SSID: "ssid", Password: "password", Token: "token", Hidden: true, WiFiType: tc.security})
		if err != nil || hex.EncodeToString(payload) != tc.payload {
			t.Fatalf("%s: %x %v", tc.security, payload, err)
		}
	}
}
