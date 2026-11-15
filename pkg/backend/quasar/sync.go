// Package quasar owns the offline device configuration, without HTTP dependencies.
package quasar

type ADBConfig struct {
	RemoteActivation bool  `json:"remoteActivation"`
	Network          bool  `json:"network"`
	FullDurationMS   int64 `json:"fullDurationMs"`
}

type TVServices struct {
	ADBEnabled                 bool `json:"adbEnabled"`
	DevelopmentSettingsEnabled bool `json:"developmentSettingsEnabled"`
}

type SystemConfig struct {
	ADBD       ADBConfig   `json:"adbd"`
	TVServices *TVServices `json:"com.yandex.tv.services.platform,omitempty"`
}

type DeviceConfig struct {
	Name string    `json:"name"`
	ADBD ADBConfig `json:"adbd"`
}

type AccountConfig struct {
	Spotter string `json:"spotter"`
}

type Config struct {
	Account AccountConfig `json:"account_config"`
	Device  DeviceConfig  `json:"device_config"`
	System  SystemConfig  `json:"system_config"`
	ADBD    ADBConfig     `json:"adbd"`
}

type Subscription struct {
	Mode string `json:"mode"`
}

type SyncInfo struct {
	Status       string       `json:"status"`
	Subscription Subscription `json:"subscription"`
	Config       Config       `json:"config"`
	ADBD         ADBConfig    `json:"adbd"`
}

func Sync(platform string) SyncInfo {
	adbd := ADBConfig{
		RemoteActivation: true,
		Network:          platform != "" && platform != "yandexstation" && platform != "yandexstation_2",
		FullDurationMS:   31536000000,
	}
	system := SystemConfig{ADBD: adbd}
	if platform == "yandexstation_2" {
		system.TVServices = &TVServices{ADBEnabled: true, DevelopmentSettingsEnabled: true}
	}
	// Firmware generations read ADB settings from different config sections.
	return SyncInfo{
		Status:       "ok",
		Subscription: Subscription{Mode: "transaction"},
		Config: Config{
			Account: AccountConfig{Spotter: "alisa"},
			Device:  DeviceConfig{Name: "quasar", ADBD: adbd},
			System:  system,
			ADBD:    adbd,
		},
		ADBD: adbd,
	}
}
