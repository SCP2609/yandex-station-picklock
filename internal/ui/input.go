package ui

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// key хранит имя клавиши либо введённый или вставленный текст.
type key struct {
	name string // Имя клавиши либо вид текстового события.
	text string // Текст символа или вставки.
}

// decoder собирает UTF-8 и управляющие последовательности из порций входных байтов.
type decoder struct {
	buffer []byte // Ещё не разобранные входные байты.
	paste  bool   // Терминал передаёт многострочную вставку.
	pasted []byte // Накопленная вставка с ограничением размера.
}

var keySequences = map[string]string{ // Известные Escape-последовательности клавиш терминала.
	"\x1b[A": "up", "\x1b[B": "down", "\x1b[C": "right", "\x1b[D": "left",
	"\x1b[H": "home", "\x1b[F": "end", "\x1b[1~": "home", "\x1b[4~": "end",
	"\x1b[3~": "delete", "\x1b[5~": "pageup", "\x1b[6~": "pagedown", "\x1b[Z": "backtab",
}

// feed разбирает доступные клавиши; незавершённые последовательности остаются в буфере.
func (d *decoder) feed(data []byte) []key {
	d.buffer = append(d.buffer, data...) // Дополняем буфер новыми входными байтами.
	var keys []key                       // Декодированные клавиши текущей порции ввода.
	for len(d.buffer) > 0 {              // Проверяем количество доступных элементов.
		if d.paste {
			const end = "\x1b[201~"                     // Маркер конца вставки в режиме bracketed paste.
			index := bytes.Index(d.buffer, []byte(end)) // Ищем маркер завершения вставки.
			if index < 0 {

				n := max(0, len(d.buffer)-len(end)+1) // Оставляем в буфере только возможное начало конечного маркера.
				d.appendPaste(d.buffer[:n])           // Сохраняем часть вставки с ограничением размера.
				d.buffer = d.buffer[n:]
				break
			}
			d.appendPaste(d.buffer[:index])                                             // Сохраняем часть вставки с ограничением размера.
			d.buffer = d.buffer[index+len(end):]                                        // Учитываем длину текущего буфера или списка.
			keys = append(keys, key{name: "paste", text: cleanInput(string(d.pasted))}) // Добавляем распознанную клавишу или вставку.
			clear(d.pasted)                                                             // Затираем использованный буфер вставки.
			d.pasted = nil
			d.paste = false
			continue
		}
		if d.buffer[0] == 27 {
			const start = "\x1b[200~"                     // Маркер начала вставки терминала.
			if bytes.HasPrefix(d.buffer, []byte(start)) { // Проверяем начало управляющей последовательности.
				d.buffer = d.buffer[len(start):] // Учитываем длину текущего буфера или списка.
				d.paste = true
				continue
			}
			matched, partial := false, strings.HasPrefix(start, string(d.buffer)) // Проверяем нужный префикс строки.
			for sequence, name := range keySequences {
				if bytes.HasPrefix(d.buffer, []byte(sequence)) { // Проверяем начало управляющей последовательности.
					keys = append(keys, key{name: name}) // Добавляем распознанную клавишу или вставку.
					d.buffer = d.buffer[len(sequence):]  // Учитываем длину текущего буфера или списка.
					matched = true
					break
				}
				partial = partial || strings.HasPrefix(sequence, string(d.buffer)) // Проверяем нужный префикс строки.
			}
			if matched {
				continue
			}
			if partial {
				break
			}

			if len(d.buffer) > 1 && d.buffer[1] == '[' { // Проверяем количество доступных элементов.
				end := 2                                                                    // Ищем конец неизвестной CSI-последовательности после Escape и «[».
				for end < len(d.buffer) && (d.buffer[end] < 0x40 || d.buffer[end] > 0x7e) { // Проверяем количество доступных элементов.
					end++
				}
				if end == len(d.buffer) && end < 32 { // Ждём продолжения короткой незавершённой CSI-команды.
					break
				}
				d.buffer = d.buffer[min(end+1, len(d.buffer)):] // Пропускаем неизвестную CSI-команду, не превращая её в ответ.
				continue
			}
			keys = append(keys, key{name: "escape"}) // Добавляем распознанную клавишу или вставку.
			d.buffer = d.buffer[1:]
			continue
		}
		b := d.buffer[0]                                                                                                                                                    // Первый ещё не разобранный байт.
		controls := map[byte]string{3: "interrupt", 4: "interrupt", 13: "enter", 10: "enter", 9: "tab", 8: "backspace", 127: "backspace", 1: "home", 5: "end", 21: "clear"} // Соответствие управляющих байтов именам клавиш.
		if name, ok := controls[b]; ok {                                                                                                                                    // Имя распознанной управляющей клавиши и признак совпадения.
			keys = append(keys, key{name: name}) // Добавляем распознанную клавишу или вставку.
			d.buffer = d.buffer[1:]
			continue
		}
		if !utf8.FullRune(d.buffer) { // Проверяем, получен ли символ UTF-8 целиком.
			break
		}
		r, size := utf8.DecodeRune(d.buffer) // Читаем символ и его длину в байтах.
		d.buffer = d.buffer[size:]
		if !unicode.IsControl(r) { // Отфильтровываем управляющие символы.
			keys = append(keys, key{name: "text", text: string(r)}) // Добавляем распознанную клавишу или вставку.
		}
	}
	return keys
}

// appendPaste сохраняет вставку в пределах 16 КиБ.
func (d *decoder) appendPaste(data []byte) {
	const limit = 16 << 10                                                     // Максимальный объём сохраняемой вставки: 16 КиБ.
	d.pasted = append(d.pasted, data[:min(len(data), limit-len(d.pasted))]...) // Сохраняем вставку в пределах ограничения размера.
}

// escapePending проверяет, ожидает ли Escape продолжения управляющей последовательности.
func (d *decoder) escapePending() bool {
	return !d.paste && len(d.buffer) > 0 && d.buffer[0] == 27 // Вне вставки начальный Escape может быть отдельной клавишей.
}

// flushEscape после таймаута выдаёт отдельную клавишу Escape.
func (d *decoder) flushEscape() []key {
	if !d.escapePending() { // Проверяем незавершённый Escape.
		return nil
	}
	d.buffer = nil
	return []key{{name: "escape"}}
}

// cleanInput заменяет управляющие символы пробелами перед вводом в диалог.
func cleanInput(value string) string {
	return strings.Map(func(r rune) rune { // Каждый управляющий символ заменяем пробелом.
		if unicode.IsControl(r) { // Отфильтровываем управляющие символы.
			return ' '
		}
		return r
	}, value)
}
