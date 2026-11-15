package execution

import (
	"context"
	"errors"
)

// Scenario объединяет имя, описание и исполняемый pipeline сценария.
type Scenario struct {
	Name        string    // Название сценария в каталоге.
	Description string    // Описание назначения сценария.
	Pipeline    *Pipeline // Подготовленное дерево исполнения.
}

// Run проверяет наличие pipeline и передаёт ему выполнение.
func (s Scenario) Run(ctx context.Context) error {
	if s.Pipeline == nil {
		return errors.New("scenario pipeline is required") // Создаём ошибку с пояснением причины.
	}
	return s.Pipeline.Execute(ctx) // Передаём выполнение pipeline сценария.
}
