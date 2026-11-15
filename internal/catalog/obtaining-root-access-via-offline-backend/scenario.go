package obtaining_root_access_via_offline_backend

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/fr33dman/yandex-station-picklock/internal/catalog/obtaining-root-access-via-offline-backend/stages"
	"github.com/fr33dman/yandex-station-picklock/pkg/execution"
	"github.com/fr33dman/yandex-station-picklock/pkg/scenario"
)

const Name = "OfflineBackendScenario"
const Description = "Настройка Wi-Fi и получение root через локальный offline backend (Station 1)"

// Scenario owns its backend lifetime; execution itself remains UI/transport independent.
type Scenario struct {
	execution.Scenario
	session *stages.Session
	started atomic.Bool
}

// NewScenario creates independent execution state for each scenario run.
// Steps come from the scenario SDK: критичный шаг задаёт NewStage, а снимаемые
// с шага флаги (Optional, BestEffort) остаются видимыми прямо в списке.
func NewScenario(config stages.Config, ui stages.Interaction, device stages.Device, backend stages.Backend) (*Scenario, error) {
	session, err := stages.NewSession(config, ui, device, backend)
	if err != nil {
		return nil, err
	}
	steps := []scenario.Stage{
		scenario.NewStage("fetch-info", "Получить модель и параметры изолированной Wi-Fi сети", stages.FetchInfo),
		scenario.NewStage("connect-wifi", "Воспроизвести сигнал подключения к Wi-Fi", stages.ConnectStationToWifi),
		scenario.NewStage("update-firmware", "Отдать проверенную OTA-прошивку Station 1", stages.UpdateStationFirmware),
		scenario.NewStage("wait-update", "Дождаться обновления и подключения ADB", stages.WaitUpdate),
		scenario.NewStage("retrieve-boot", "Сохранить и проверить оригинальный boot", stages.RetrieveBoot),
		scenario.NewStage("prepare-boot", "Подготовить проверенный boot с ADB и временным Telnet", stages.PrepareBoot),
		scenario.NewStage("write-boot", "Записать boot и проверить полный readback", stages.WriteBoot),
		scenario.NewStage("reboot", "Перезагрузить колонку после подтверждения", stages.Reboot),
		scenario.NewStage("wait-reboot", "Проверить ADB и полный root через Telnet", stages.WaitReboot),
		scenario.NewStage("install-ssh", "Установить Dropbear и проверить вход по ключу", stages.AddDropbear),
		scenario.NewStage("disable-telnet", "Удалить Telnet из boot и остановить текущий сервис", stages.DisableTelnet),
		// Ограничение ADB сетью 192.168.0.0/16 — политика пользователя:
		// шаг можно пропустить, и его сбой не останавливает остальные.
		scenario.NewStage("restrict-adb", "Ограничить TCP/5555 сетью 192.168.0.0/16", stages.UpdateFirewall).Optional().BestEffort(),
	}
	base, err := scenario.NewScenario(Name, Description, steps...)
	if err != nil {
		return nil, err
	}
	return &Scenario{Scenario: *base, session: session}, nil
}

func (s *Scenario) BackupDirectory() string { return s.session.BackupDirectory() }

func (s *Scenario) Run(ctx context.Context) (err error) {
	if !s.started.CompareAndSwap(false, true) {
		return execution.ErrAlreadyStarted
	}
	defer func() {
		// Cleanup must run even when the user's execution context is canceled.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		err = errors.Join(err, s.session.Backend.Close(cleanup))
	}()
	return s.Scenario.Run(stages.WithSession(ctx, s.session))
}
