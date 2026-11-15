package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// playWAV выбирает системный проигрыватель, воспроизводит временный WAV и удаляет его.
// Отмена контекста останавливает процесс проигрывателя.
func playWAV(ctx context.Context, wav []byte) (result error) {
	if err := ctx.Err(); err != nil { // Проверяем отмену или истечение срока контекста.
		return err
	}
	programs := [][]string{{"aplay", "-q"}, {"paplay"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "error"}} // Кандидаты проигрывателя с их аргументами.
	if runtime.GOOS == "darwin" {
		programs = [][]string{{"afplay"}}
	}
	var program string // Путь первого найденного проигрывателя.
	var args []string  // Аргументы выбранного проигрывателя.
	for _, candidate := range programs {
		if found, err := exec.LookPath(candidate[0]); err == nil { // Ищем установленный системный проигрыватель.
			program = found
			args = candidate[1:]
			break
		}
	}
	if program == "" {
		return errors.New("audio player not found: install afplay (macOS), aplay, paplay or ffplay (Linux)") // Создаём ошибку с пояснением причины.
	}
	directory, err := os.MkdirTemp("", "picklocker-audio-") // Создаём приватный временный каталог для WAV.
	if err != nil {
		return err
	}
	defer func() { // При выходе удаляем временные данные и сохраняем ошибку очистки.
		result = errors.Join(result, os.RemoveAll(directory)) // Объединяем ошибки, сохраняя каждую причину.
	}()
	filename := filepath.Join(directory, "pairing.wav")       // Собираем путь с разделителями текущей ОС.
	if err := os.WriteFile(filename, wav, 0600); err != nil { // Сохраняем WAV с доступом только для владельца.
		return err
	}
	command := exec.CommandContext(ctx, program, append(args, filename)...) // Привязываем процесс проигрывателя к отмене контекста.
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput() // Ждём проигрыватель и собираем его диагностический вывод.
	if err != nil {
		return fmt.Errorf("audio playback: %w: %s", errors.Join(err, ctx.Err()), strings.TrimSpace(string(output))) // Добавляем пояснение к возвращаемой ошибке.
	}
	return nil
}
