package ui

import (
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// color задаёт индекс стиля в терминальной палитре.
type color uint8

const (
	normal color = iota
	muted
	purple
	green
	yellow
	red
	cyan
	selected
)

var colors = []string{"\x1b[0m", "\x1b[0;38;5;245m", "\x1b[0;38;5;141m", "\x1b[0;38;5;114m", "\x1b[0;38;5;220m", "\x1b[0;38;5;203m", "\x1b[0;38;5;117m", "\x1b[0;1;38;5;231;48;5;60m"} // ANSI-последовательности стилей в порядке значений color.

// cell хранит текст и цвет одной экранной ячейки.
type cell struct {
	text  string // Символы ячейки; пустая строка продолжает широкий символ.
	color color  // Индекс стиля ячейки.
}

// rect хранит координаты x/y и размеры w/h прямоугольника в ячейках.
type rect struct{ x, y, w, h int }

// canvas хранит сетку ячеек будущего кадра.
type canvas struct {
	w, h int      // Размер сетки в ячейках.
	rows [][]cell // Строки ячеек будущего кадра.
}

// newCanvas создаёт непустую сетку и заполняет её пробелами.
func newCanvas(w, h int) *canvas {
	c := &canvas{w: max(w, 1), h: max(h, 1)} // Даже при нулевом размере оставляем хотя бы одну ячейку.
	c.rows = make([][]cell, c.h)             // Выделяем ячейки сетки кадра.
	for y := range c.rows {
		c.rows[y] = make([]cell, c.w) // Выделяем ячейки сетки кадра.
		for x := range c.rows[y] {
			c.rows[y][x].text = " "
		}
	}
	return c
}

// runeWidth оценивает ширину символа в терминальных ячейках: ноль, один или два.
func runeWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == '\u200d' { // Проверяем символы без собственной ширины.
		return 0
	}
	switch width.LookupRune(r).Kind() { // Определяем, занимает ли символ две ячейки.
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// textWidth считает ширину текста без управляющих символов.
func textWidth(text string) int {
	n := 0 // Накопленная ширина текста в ячейках.
	for _, r := range text {
		if !unicode.IsControl(r) { // Отфильтровываем управляющие символы.
			n += runeWidth(r) // Считаем ширину символа в ячейках.
		}
	}
	return n
}

// text рисует строку в границах области; пользовательский текст не исполняется как ANSI.
func (c *canvas) text(x, y, limit int, text string, style color) {
	if y < 0 || y >= c.h || x < 0 {
		return
	}

	end := min(c.w, x+max(limit, 0)) // Правая граница текста с учётом ширины холста.
	previous := -1                   // Ячейки для присоединяемого символа пока нет.

	for _, r := range text {
		if unicode.IsControl(r) { // Отфильтровываем управляющие символы.
			continue
		}
		w := runeWidth(r) // Считаем ширину символа в ячейках.
		if w == 0 {
			if previous >= 0 {
				c.rows[y][previous].text += string(r) // Преобразуем символы или байты в строку.
			}
			continue
		}
		if x+w > end {
			break
		}
		c.rows[y][x] = cell{text: string(r), color: style} // Преобразуем символы или байты в строку.
		previous = x
		for i := 1; i < w; i++ { // Помечаем ячейки, занятые продолжением широкого символа.
			c.rows[y][x+i] = cell{text: "", color: style}
		}
		x += w
	}
}

// box рисует рамку с необязательным заголовком.
func (c *canvas) box(r rect, title string, style color) {
	if r.w < 2 || r.h < 2 {
		return
	}
	c.text(r.x, r.y, r.w, "╭"+strings.Repeat("─", r.w-2)+"╮", style)       // Рисуем текст в пределах указанной области.
	c.text(r.x, r.y+r.h-1, r.w, "╰"+strings.Repeat("─", r.w-2)+"╯", style) // Рисуем текст в пределах указанной области.
	for y := r.y + 1; y < r.y+r.h-1; y++ {                                 // Вертикальная координата следующей строки.
		c.text(r.x, y, 1, "│", style)       // Рисуем текст в пределах указанной области.
		c.text(r.x+r.w-1, y, 1, "│", style) // Рисуем текст в пределах указанной области.
	}
	if title != "" {
		c.text(r.x+2, r.y, r.w-4, " "+title+" ", style) // Рисуем текст в пределах указанной области.
	}
}

// paragraph переносит текст, рисует доступные строки и возвращает их количество.
func (c *canvas) paragraph(r rect, text string, style color) int {
	lines := wrap(text, r.w)                                    // Разбиваем текст на строки доступной ширины.
	for i, line := range lines[:min(len(lines), max(r.h, 0))] { // Ограничиваем размер доступной областью.
		c.text(r.x, r.y+i, r.w, line, style) // Рисуем текст в пределах указанной области.
	}
	return min(len(lines), max(r.h, 0)) // Возвращаем количество реально нарисованных строк.
}

// wrap переносит текст по ширине терминальных ячеек с сохранением явных переводов строк.
func wrap(text string, columns int) []string {
	if columns < 1 {
		return nil
	}
	var lines []string                                                                  // Строки подготовленного текста.
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") { // Разделяем текст по явным переводам строк.
		var line strings.Builder // Накопитель одной строки текста.
		used := 0                // Количество уже занятых ячеек строки.
		for _, r := range paragraph {
			if unicode.IsControl(r) { // Отфильтровываем управляющие символы.
				r = ' '
			}
			n := runeWidth(r) // Считаем ширину символа в ячейках.
			if used+n > columns {
				lines = append(lines, line.String()) // Сохраняем готовую строку в результате.
				line.Reset()                         // Очищаем накопитель следующей строки.
				used = 0
			}
			line.WriteRune(r) // Добавляем символ к текущей строке.
			used += n
		}
		lines = append(lines, line.String()) // Сохраняем готовую строку в результате.
	}
	return lines
}

// frame превращает сетку ячеек в кадр с ANSI-цветами и очисткой концов строк.
func (c *canvas) frame() string {
	var result strings.Builder          // Накопитель ANSI-команд и текста кадра.
	result.WriteString("\x1b[H")        // Переводим курсор к началу кадра без прокрутки экрана.
	current := normal                   // Текущий ANSI-стиль вывода.
	result.WriteString(colors[current]) // Дописываем часть готового кадра.
	for y, row := range c.rows {
		for _, cell := range row {
			if cell.color != current {
				current = cell.color
				result.WriteString(colors[current]) // Дописываем часть готового кадра.
			}
			result.WriteString(cell.text) // Дописываем часть готового кадра.
		}
		result.WriteString("\x1b[0m\x1b[K") // Сбрасываем стиль и стираем оставшийся хвост строки.
		current = normal
		if y+1 < c.h {
			result.WriteString("\r\n") // Переходим на следующую строку внутри кадра.
		}
	}
	return result.String() // Возвращаем накопленный кадр.
}
