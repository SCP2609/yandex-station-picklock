package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/firmware"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

// serveBackend запускает отдельный HTTP/TLS-бэкенд и закрывает его после отмены или сбоя.
func serveBackend(ctx context.Context, o options) error {
	var update *ota.Update // OTA-ответ; nil означает отсутствие обновления.
	if o.firmware != "" {
		image, err := firmware.NewFile(o.firmware, o.digest) // Открываем OTA-образ и проверяем его хеш.
		if err != nil {
			return err
		}
		o.release.Critical = true
		update, err = ota.NewUpdate(o.release, image) // Связываем параметры OTA с образом прошивки.
		if err != nil {
			return err
		}
	} else if o.digest != "" || o.release != (ota.Release{}) {
		return errors.New("OTA flags require -firmware") // Создаём ошибку с пояснением причины.
	}
	config, err := listenerConfig(o) // Собираем параметры HTTP/TLS-слушателей.
	if err != nil {
		return err
	}
	server, err := backend.NewController(config) // Создаём контроллер сменяемого бэкенда.
	if err != nil {
		return err
	}
	if err := server.Replace(ctx, update); err != nil { // Запускаем сервер с выбранным OTA-ответом.
		return err
	}
	slog.Info("offline backend started", "address", o.listen, "https", config.HTTPSAddress, "ota", update != nil) // Записываем параметры запущенного бэкенда.
	failure := server.Wait(ctx)                                                                                   // Ждём отмены либо ошибки сервера.
	if errors.Is(failure, ctx.Err()) {                                                                            // Проверяем причину с учётом обёрток ошибки.
		failure = nil
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second) // Ограничиваем время корректного завершения.
	defer cancel()                                                                // Отменяем дочерний контекст и освобождаем ресурсы.
	return errors.Join(failure, server.Close(shutdown))                           // Объединяем ошибки, сохраняя каждую причину.
}
