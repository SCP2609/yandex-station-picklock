package obtaining_root_access_via_offline_backend

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/fr33dman/yandex-station-picklock/internal/catalog/obtaining-root-access-via-offline-backend/stages"
	"github.com/fr33dman/yandex-station-picklock/internal/execution"
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
func NewScenario(config stages.Config, ui stages.Interaction, device stages.Device, backend stages.Backend) (*Scenario, error) {
	session, err := stages.NewSession(config, ui, device, backend)
	if err != nil {
		return nil, err
	}
	steps := []execution.Stage{
		{Name: "fetch-info", Description: "Получить модель и параметры изолированной Wi-Fi сети", Executive: stages.FetchInfo},
		{Name: "connect-wifi", Description: "Воспроизвести сигнал подключения к Wi-Fi", Executive: stages.ConnectStationToWifi},
		{Name: "update-firmware", Description: "Отдать проверенную OTA-прошивку Station 1", Executive: stages.UpdateStationFirmware},
		{Name: "wait-update", Description: "Дождаться обновления и подключения ADB", Executive: stages.WaitUpdate},
		{Name: "retrieve-boot", Description: "Сохранить и проверить оригинальный boot", Executive: stages.RetrieveBoot},
		{Name: "prepare-boot", Description: "Подготовить проверенный boot с ADB и временным Telnet", Executive: stages.PrepareBoot},
		{Name: "write-boot", Description: "Записать boot и проверить полный readback", Executive: stages.WriteBoot},
		{Name: "reboot", Description: "Перезагрузить колонку после подтверждения", Executive: stages.Reboot},
		{Name: "wait-reboot", Description: "Проверить ADB и полный root через Telnet", Executive: stages.WaitReboot},
		{Name: "install-ssh", Description: "Установить Dropbear и проверить вход по ключу", Executive: stages.AddDropbear},
		{Name: "disable-telnet", Description: "Удалить Telnet из boot и остановить текущий сервис", Executive: stages.DisableTelnet},
		{Name: "restrict-adb", Description: "Ограничить TCP/5555 сетью 192.168.0.0/16", Executive: stages.UpdateFirewall},
	}
	jobs := make([]execution.Job, 0, len(steps))
	for _, step := range steps {
		jobs = append(jobs, execution.NewSingleJob(step, nil))
	}
	pipeline, err := execution.NewPipeline(jobs...)
	if err != nil {
		return nil, err
	}
	return &Scenario{Scenario: execution.Scenario{
		Name:        Name,
		Description: Description,
		Pipeline:    pipeline,
	}, session: session}, nil
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
