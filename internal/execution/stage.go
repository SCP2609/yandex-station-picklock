package execution

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
)

// Stage описывает один шаг; состояние выполнения и восстановление хранит его Job.
type Stage struct {
	Name        string                      // Название шага для статуса и ошибок.
	Description string                      // Описание действий шага для UI.
	Executive   func(context.Context) error // Действие шага; отмена передаётся через контекст.
}

// Execute запускает шаг, учитывает отмену и превращает панику в ошибку со стеком.
// Значения контекста передаются без изменений; общий изменяемый объект требует синхронизации.
func (s Stage) Execute(ctx context.Context) (err error) {
	defer func() { // При выходе перехватываем панику и добавляем имя шага к ошибке.
		if value := recover(); value != nil { // Перехватываем панику для возврата обычной ошибки.
			err = fmt.Errorf("panic: %v\n%s", value, debug.Stack()) // Добавляем пояснение к возвращаемой ошибке.
		}
		if err != nil {
			err = fmt.Errorf("stage %q: %w", s.Name, err) // Добавляем пояснение к возвращаемой ошибке.
		}
	}()
	if err := ctx.Err(); err != nil { // Проверяем отмену или истечение срока контекста.
		return err
	}
	if s.Executive == nil {
		return errors.New("executive is required") // Создаём ошибку с пояснением причины.
	}
	err = s.Executive(ctx)            // Выполняем действие шага с исходным контекстом.
	return withCancellation(ctx, err) // Сохраняем ошибку операции вместе с причиной отмены.
}
