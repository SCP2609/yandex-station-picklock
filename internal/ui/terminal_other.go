//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package ui

import (
	"context"
	"errors"
)

// inputEvent сохраняет общий формат событий ввода на неподдерживаемых платформах.
type inputEvent struct {
	data []byte // Собственная копия прочитанных байтов.
	err  error  // Ошибка чтения либо закрытие ввода.
}

// openTerminal сообщает, что платформа не поддерживает полноэкранный терминал.
func openTerminal() (*terminal, error) {
	return nil, errors.New("fullscreen UI requires a Unix terminal") // Создаём ошибку с пояснением причины.
}

// read служит заглушкой; на этой платформе терминал не открывается.
func (*terminal) read(context.Context, chan<- any) {}
