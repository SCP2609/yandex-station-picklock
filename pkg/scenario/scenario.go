// Package scenario is the public SDK for scenario authors: stage constructors
// with safe defaults and ready-made steps such as Wi-Fi pairing or waiting for a
// device. It depends only on public packages (pkg/execution, pkg/quasarsound), so
// scenarios can live outside this repository. Scenarios may use these pieces or
// describe their own Stage values by hand; both forms feed the same pipeline.
package scenario

import (
	"context"

	"github.com/fr33dman/yandex-station-picklock/pkg/execution"
)

type (
	// Stage описывает шаг сценария; это тот же тип, что и execution.Stage.
	Stage = execution.Stage
	// Step — действие шага, получающее контекст с отменой задачи.
	Step = func(context.Context) error
	// Interaction — единственная точка общения шага с UI.
	Interaction = execution.Interaction
	// Question — текстовый вопрос пользователю.
	Question = execution.Question
	// Job — узел дерева выполнения: шаг либо группа шагов с запасным путём.
	// Интерфейс запечатан движком: сценарий собирает дерево из готовых узлов.
	Job = execution.Job
	// SingleJob выполняет один Stage и при ошибке запускает запасную задачу.
	SingleJob = execution.SingleJob
	// ParallelJobs запускает дочерние задачи параллельно и ждёт завершения каждой.
	ParallelJobs = execution.ParallelJobs
	// Pipeline — дерево выполнения одного запуска снимков состояния.
	Pipeline = execution.Pipeline
	// Scenario — именованный сценарий с готовым pipeline.
	Scenario = execution.Scenario
	// OptionalDecision решает, запускать ли необязательный шаг.
	OptionalDecision = execution.OptionalDecision
)

// NewStage создаёт критичный шаг: его сбой останавливает весь pipeline.
func NewStage(name, description string, step Step) Stage {
	return execution.NewStage(name, description, step)
}

// NewSingleJob оборачивает шаг в задачу с необязательным запасным путём.
func NewSingleJob(stage Stage, fallback Job) *SingleJob {
	return execution.NewSingleJob(stage, fallback)
}

// NewParallelJobs объединяет задачи в параллельную группу с общим запасным путём.
func NewParallelJobs(jobs []Job, fallback Job) *ParallelJobs {
	return execution.NewParallelJobs(jobs, fallback)
}

// NewPipeline собирает шаги в последовательный pipeline.
func NewPipeline(stages ...Stage) (*Pipeline, error) {
	return execution.NewPipelineFromStages(stages...)
}

// NewPipelineFromJobs собирает pipeline из произвольного дерева задач.
func NewPipelineFromJobs(jobs ...Job) (*Pipeline, error) {
	return execution.NewPipeline(jobs...)
}

// NewScenario собирает сценарий из последовательных шагов.
func NewScenario(name, description string, stages ...Stage) (*Scenario, error) {
	pipeline, err := NewPipeline(stages...)
	if err != nil {
		return nil, err
	}
	return &Scenario{Name: name, Description: description, Pipeline: pipeline}, nil
}

// WithOptionalDecision подключает к контексту выбор пользователя для необязательных шагов.
// UI передаёт сюда свой Confirm; без решения необязательный шаг выполняется.
func WithOptionalDecision(ctx context.Context, decide OptionalDecision) context.Context {
	return execution.WithOptionalDecision(ctx, decide)
}
