//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// openTerminal проверяет TTY, дублирует дескрипторы и включает полноэкранный режим.
// Исходные stdin/stdout остаются открыты; повторное открытие /dev/tty не требуется.
func openTerminal() (*terminal, error) {
	if os.Getenv("TERM") == "dumb" { // Проверяем заявленный тип терминала.
		return nil, terminalError(errors.New("TERM=dumb")) // Добавляем пояснение о требованиях к терминалу.
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) { // Проверяем, подключены ли ввод и вывод к TTY.
		return nil, terminalError(errors.New("stdin and stdout must be terminals")) // Добавляем пояснение о требованиях к терминалу.
	}

	inputFD, err := unix.FcntlInt(os.Stdin.Fd(), unix.F_DUPFD_CLOEXEC, 0) // Дублируем stdin, чтобы закрывать только собственный дескриптор.
	if err != nil {
		return nil, terminalError(err) // Добавляем пояснение о требованиях к терминалу.
	}
	file := os.NewFile(uintptr(inputFD), "picklocker-input")                // Оборачиваем собственную копию дескриптора в файл.
	outputFD, err := unix.FcntlInt(os.Stdout.Fd(), unix.F_DUPFD_CLOEXEC, 0) // Дублируем stdout с автоматическим закрытием при exec.
	if err != nil {
		_ = file.Close()               // Закрываем созданную копию ввода при ошибке.
		return nil, terminalError(err) // Добавляем пояснение о требованиях к терминалу.
	}
	output := os.NewFile(uintptr(outputFD), "picklocker-output") // Оборачиваем собственную копию дескриптора в файл.
	state, err := term.MakeRaw(int(file.Fd()))                   // Включаем посимвольный ввод без эха и обработки терминалом.
	if err != nil {
		_ = file.Close()               // Закрываем созданную копию ввода при ошибке.
		_ = output.Close()             // Закрываем созданную копию вывода при ошибке.
		return nil, terminalError(err) // Добавляем пояснение о требованиях к терминалу.
	}
	t := &terminal{file: file, output: output, state: state, resize: make(chan os.Signal, 1)} // Буфер одного результата не задерживает завершение отправителя.
	if err := t.write(enterScreen); err != nil {                                              // Выводим кадр или управляющую команду в терминал.
		_ = t.close() // Восстанавливаем режимы терминала и закрываем его копии дескрипторов.
		return nil, err
	}
	signal.Notify(t.resize, syscall.SIGWINCH) // Подписываемся на изменение размера терминала.
	return t, nil
}

// inputEvent передаёт прочитанные байты или ошибку в событийный цикл.
type inputEvent struct {
	data []byte // Собственная копия прочитанных байтов.
	err  error  // Ошибка чтения либо закрытие ввода.
}

// read опрашивает ввод с таймаутом, чтобы завершиться после отмены контекста.
func (t *terminal) read(ctx context.Context, events chan<- any) {
	buffer := make([]byte, 4096) // Выделяем буфер нужной длины.
	for ctx.Err() == nil {       // Проверяем отмену между опросами ввода.
		fds := []unix.PollFd{{Fd: int32(t.file.Fd()), Events: unix.POLLIN}} // Приводим дескриптор к типу, ожидаемому системным API.
		n, err := unix.Poll(fds, 100)                                       // Ждём ввода не дольше 100 мс для своевременной отмены.
		if errors.Is(err, unix.EINTR) {                                     // Прерывание сигналом не считается ошибкой чтения.
			continue
		}
		if err != nil {
			sendEvent(ctx, events, inputEvent{err: err}) // Передаём байты либо ошибку в событийный цикл.
			return
		}
		if n == 0 {
			continue
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			sendEvent(ctx, events, inputEvent{err: io.EOF}) // Передаём байты либо ошибку в событийный цикл.
			return
		}
		n, err = t.file.Read(buffer) // Читаем очередную порцию терминального ввода.
		if n > 0 {
			sendEvent(ctx, events, inputEvent{data: bytes.Clone(buffer[:n])}) // Передаём копию байтов: буфер чтения будет переиспользован.
		}
		if err != nil {
			sendEvent(ctx, events, inputEvent{err: err}) // Передаём байты либо ошибку в событийный цикл.
			return
		}
	}
}

// sendEvent доставляет событие, пока контекст чтения не отменён.
func sendEvent(ctx context.Context, events chan<- any, event any) {
	select {
	case events <- event:
	case <-ctx.Done(): // Ожидаем отмены контекста.
	}
}
