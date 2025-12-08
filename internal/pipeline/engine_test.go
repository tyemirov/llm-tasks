package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tyemirov/llm-tasks/internal/pipeline"
)

type fakeClient struct {
	responses []string
	call      int
}

func (f *fakeClient) Chat(ctx context.Context, req pipeline.LLMRequest) (pipeline.LLMResponse, error) {
	if f.call >= len(f.responses) {
		return pipeline.LLMResponse{}, errors.New("no more responses")
	}
	r := f.responses[f.call]
	f.call++
	return pipeline.LLMResponse{RawText: r}, nil
}

type fakePipeline struct {
	gathered any
	verify   func(g any, r pipeline.LLMResponse) (bool, any, *pipeline.RefineRequest, error)
	applied  bool
}

func (p *fakePipeline) Name() string { return "fake" }
func (p *fakePipeline) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	p.gathered = []int{1, 2, 3}
	return p.gathered, nil
}
func (p *fakePipeline) Prompt(ctx context.Context, g pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	return pipeline.LLMRequest{UserPrompt: "hi"}, nil
}
func (p *fakePipeline) Verify(ctx context.Context, g pipeline.GatherOutput, r pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	return p.verify(g, r)
}
func (p *fakePipeline) Apply(ctx context.Context, v pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	p.applied = true
	return pipeline.ApplyReport{DryRun: false, Summary: "ok", NumActions: 1}, nil
}

func TestRunner_RefineFlow(t *testing.T) {
	fp := &fakePipeline{
		verify: func(g any, r pipeline.LLMResponse) (bool, any, *pipeline.RefineRequest, error) {
			if r.RawText == "bad" {
				return false, nil, &pipeline.RefineRequest{UserPromptDelta: "fix"}, nil
			}
			if r.RawText == "good" {
				return true, "verified", nil, nil
			}
			return false, nil, nil, errors.New("unexpected")
		},
	}
	client := &fakeClient{responses: []string{"bad", "good"}}
	r := pipeline.Runner{
		Client:  client,
		Options: pipeline.RunOptions{MaxAttempts: 3, Timeout: 2 * time.Second},
	}
	_, err := r.Run(context.Background(), fp)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fp.applied {
		t.Fatalf("expected Apply to be called")
	}
}

func TestRunner_ExhaustAttempts(t *testing.T) {
	fp := &fakePipeline{
		verify: func(g any, r pipeline.LLMResponse) (bool, any, *pipeline.RefineRequest, error) {
			return false, nil, &pipeline.RefineRequest{UserPromptDelta: "again"}, nil
		},
	}
	client := &fakeClient{responses: []string{"bad1", "bad2"}}
	r := pipeline.Runner{
		Client:  client,
		Options: pipeline.RunOptions{MaxAttempts: 2, Timeout: time.Second},
	}
	_, err := r.Run(context.Background(), fp)
	if err == nil {
		t.Fatalf("expected error after exhausting attempts")
	}
}
