package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"golang.org/x/term"
)

// terminal владеет копиями дескрипторов, сохранённым режимом ввода и каналом изменения размера.
type terminal struct {
	file   *os.File       // Собственная копия дескриптора ввода.
	output *os.File       // Собственная копия дескриптора вывода.
	state  *term.State    // Режим ввода до переключения в raw.
	resize chan os.Signal // Сигналы изменения размера терминала.
}

const enterScreen = "\x1b[?1049h\x1b[?25l\x1b[?2004h\x1b[2J" // Запасной экран, скрытый курсор и режим безопасной вставки.
const leaveScreen = "\x1b[?2004l\x1b[?25h\x1b[0m\x1b[?1049l" // Возврат основного экрана, курсора и обычного ввода.

// size возвращает размеры терминала в ячейках.
func (t *terminal) size() (int, int, error) {
	return term.GetSize(int(t.file.Fd())) // Читаем размеры терминала в ячейках.
}

// write выводит полный кадр и обнаруживает неполную запись.
func (t *terminal) write(frame string) error {
	n, err := io.WriteString(t.output, frame) // Проверяем не только ошибку, но и полноту записи кадра.
	if err == nil && n != len(frame) {        // Проверяем количество доступных элементов.
		err = io.ErrShortWrite
	}
	return err
}

// close восстанавливает экран и режим ввода, затем закрывает собственные дескрипторы.
// Все действия выполняются, даже если одно из них вернуло ошибку.
func (t *terminal) close() error {
	signal.Stop(t.resize) // Отменяем доставку сигналов изменения размера.

	return errors.Join(t.write(leaveScreen), term.Restore(int(t.file.Fd()), t.state), t.file.Close(), t.output.Close()) // Пробуем все операции восстановления и сохраняем каждую ошибку.
}

// terminalError добавляет к ошибке пояснение о требованиях к терминалу.
func terminalError(err error) error {
	return fmt.Errorf("Picklocker requires an interactive ANSI terminal: %w", err) // Добавляем пояснение к возвращаемой ошибке.
}
