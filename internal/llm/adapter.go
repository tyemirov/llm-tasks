package llm

import (
	"context"
	"strings"

	"github.com/tyemirov/llm-tasks/internal/pipeline"
)

type Adapter struct {
	Client              Client
	DefaultModel        string
	DefaultTemp         float64
	DefaultTokens       int
	SupportsTemperature bool
}

func (a Adapter) Chat(ctx context.Context, req pipeline.LLMRequest) (pipeline.LLMResponse, error) {
	model := req.Model
	if strings.TrimSpace(model) == "" {
		model = a.DefaultModel
	}

	var tempPtr *float64
	if a.SupportsTemperature {
		// Use request.Temperature if present, else adapter default
		t := chooseFloat(req.Temperature, a.DefaultTemp)
		tempPtr = &t
	}

	cr := ChatCompletionRequest{
		Model:               model,
		Messages:            []ChatMessage{{Role: "system", Content: strings.TrimSpace(req.SystemPrompt)}, {Role: "user", Content: strings.TrimSpace(req.UserPrompt)}},
		MaxCompletionTokens: chooseInt(req.MaxTokens, a.DefaultTokens),
		Temperature:         tempPtr, // omitted if nil
	}

	out, err := a.Client.CreateChatCompletion(ctx, cr)
	if err != nil {
		return pipeline.LLMResponse{}, err
	}
	return pipeline.LLMResponse{RawText: out}, nil
}

func chooseInt(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}
func chooseFloat(a, b float64) float64 {
	if a > 0 {
		return a
	}
	return b
}
