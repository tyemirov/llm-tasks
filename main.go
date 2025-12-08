package main

import (
	"os"

	llmtasks "github.com/tyemirov/llm-tasks/cmd/llm-tasks"
	"go.uber.org/zap"
)

func main() {
	logger := zap.Must(zap.NewProduction())

	executionErr := llmtasks.Execute()
	if executionErr != nil {
		logger.Error("command execution failed", zap.Error(executionErr))
		_ = logger.Sync()
		os.Exit(1)
	}

	syncErr := logger.Sync()
	if syncErr != nil {
		os.Exit(1)
	}
}
