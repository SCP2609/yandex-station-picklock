package execution

import "context"

// Question описывает текстовый запрос, начальное значение и необходимость скрывать ввод.
type Question struct {
	Text    string // Текст вопроса пользователю.
	Default string // Начальное редактируемое значение.
	Secret  bool   // Признак маскировки вводимых символов.
}

// Interaction связывает шаги с UI. Play ждёт конца звука; исходный WAV остаётся у вызывающего кода.
type Interaction interface {
	Ask(context.Context, Question) (string, error) // Ask запрашивает текст и ждёт ответа либо отмены.
	Confirm(context.Context, string) (bool, error) // Confirm ждёт явного согласия или отказа.
	Play(context.Context, []byte) error            // Play ждёт конца звука, не забирая исходный буфер WAV.
}

// TaskInfo связывает запрос к пользователю с выполняемой задачей.
type TaskInfo struct {
	ID          string // Идентификатор задачи в текущем дереве.
	Name        string // Имя выполняемого шага.
	Description string // Описание шага для панели UI.
}

// taskKey служит закрытым ключом контекста для метаданных задачи.
type taskKey struct{}

// CurrentTask извлекает задачу из контекста, независимо от частоты опроса pipeline.
func CurrentTask(ctx context.Context) (TaskInfo, bool) {
	task, ok := ctx.Value(taskKey{}).(TaskInfo) // Читаем значение по закрытому ключу контекста.
	return task, ok
}
