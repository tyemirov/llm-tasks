package llmtasks

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tyemirov/llm-tasks/internal/config"
	"github.com/tyemirov/llm-tasks/internal/llm"
	"github.com/tyemirov/llm-tasks/internal/pipeline"
	changelogtask "github.com/tyemirov/llm-tasks/tasks/changelog"
	sorttask "github.com/tyemirov/llm-tasks/tasks/sort"
)

type pipelineBuilder func(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error)

var pipelineBuilders = map[string]pipelineBuilder{
	sortRecipeType:      buildSortPipeline,
	changelogRecipeType: buildChangelogPipeline,
}

func runTaskCommand(command *cobra.Command, options runCommandOptions) error {
	rootConfiguration, err := loadRootConfiguration(options.configPath)
	if err != nil {
		return err
	}

	targetRecipe, recipeFound := rootConfiguration.FindRecipe(options.taskName)
	if !recipeFound || !targetRecipe.Enabled {
		return fmt.Errorf("unknown or disabled recipe %q", options.taskName)
	}

	var mappedChangelogConfig *config.ChangelogConfig
	if targetRecipe.Type == changelogRecipeType {
		changelogConfig, mapErr := config.MapChangelog(targetRecipe)
		if mapErr != nil {
			return fmt.Errorf("map changelog recipe %s: %w", targetRecipe.Name, mapErr)
		}
		mappedChangelogConfig = &changelogConfig

		trimmedVersion := strings.TrimSpace(options.changelogVersion)
		if trimmedVersion != "" && strings.TrimSpace(changelogConfig.Inputs.Version.Env) != "" {
			if setErr := os.Setenv(changelogConfig.Inputs.Version.Env, trimmedVersion); setErr != nil {
				return fmt.Errorf(setEnvironmentVariableErrorFormat, changelogConfig.Inputs.Version.Env, setErr)
			}
		}

		trimmedDate := strings.TrimSpace(options.changelogDate)
		if trimmedDate != "" && strings.TrimSpace(changelogConfig.Inputs.Date.Env) != "" {
			if setErr := os.Setenv(changelogConfig.Inputs.Date.Env, trimmedDate); setErr != nil {
				return fmt.Errorf(setEnvironmentVariableErrorFormat, changelogConfig.Inputs.Date.Env, setErr)
			}
		}
	}

	selectedModelName := resolveModelName(options, targetRecipe, rootConfiguration)
	modelConfiguration, modelFound := rootConfiguration.FindModel(selectedModelName)
	if !modelFound {
		return fmt.Errorf("model %q not found in models[]", selectedModelName)
	}

	apiKeyEnvironmentVariable := strings.TrimSpace(rootConfiguration.Common.API.APIKeyEnv)
	if apiKeyEnvironmentVariable == "" {
		apiKeyEnvironmentVariable = defaultAPIKeyEnvironmentVariable
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnvironmentVariable))
	if apiKey == "" {
		return fmt.Errorf("missing API key: set %s", apiKeyEnvironmentVariable)
	}

	apiEndpoint := strings.TrimSpace(rootConfiguration.Common.API.Endpoint)
	if apiEndpoint == "" {
		apiEndpoint = defaultAPIEndpoint
	}

	httpClient := llm.Client{
		HTTPBaseURL:       apiEndpoint,
		APIKey:            apiKey,
		ModelIdentifier:   modelConfiguration.ModelID,
		MaxTokensResponse: modelConfiguration.MaxCompletionTokens,
		Temperature:       modelConfiguration.DefaultTemperature,
	}
	adapter := llm.Adapter{
		Client:              httpClient,
		DefaultModel:        modelConfiguration.ModelID,
		DefaultTemp:         modelConfiguration.DefaultTemperature,
		DefaultTokens:       modelConfiguration.MaxCompletionTokens,
		SupportsTemperature: modelConfiguration.SupportsTemperature,
	}

	effectiveAttempts := rootConfiguration.Common.Defaults.Attempts
	if options.attempts > 0 {
		effectiveAttempts = options.attempts
	}
	if effectiveAttempts <= 0 {
		effectiveAttempts = 3
	}

	effectiveTimeout := time.Duration(rootConfiguration.Common.Defaults.TimeoutSeconds) * time.Second
	if options.timeout > 0 {
		effectiveTimeout = options.timeout
	}
	if effectiveTimeout <= 0 {
		effectiveTimeout = 45 * time.Second
	}

	runner := pipeline.Runner{
		Client: adapter,
		Options: pipeline.RunOptions{
			MaxAttempts: effectiveAttempts,
			DryRun:      false,
			Timeout:     effectiveTimeout,
		},
	}

	taskPipeline, builderErr := buildPipeline(rootConfiguration, targetRecipe, mappedChangelogConfig)
	if builderErr != nil {
		return builderErr
	}

	executionContext := command.Context()
	report, runErr := runner.Run(executionContext, taskPipeline)
	if runErr != nil {
		return fmt.Errorf("run pipeline %s: %w", targetRecipe.Name, runErr)
	}

	_, writeErr := fmt.Fprintf(command.OutOrStdout(), "%s (actions=%d, dry=%v)\n", report.Summary, report.NumActions, report.DryRun)
	if writeErr != nil {
		return fmt.Errorf("write run result: %w", writeErr)
	}

	return nil
}

func resolveModelName(options runCommandOptions, recipe config.Recipe, root config.Root) string {
	modelName := strings.TrimSpace(options.modelOverride)
	if modelName != "" {
		return modelName
	}

	recipeModel := strings.TrimSpace(recipe.Model)
	if recipeModel != "" {
		return recipeModel
	}

	defaultModel, ok := root.DefaultModel()
	if ok {
		return defaultModel.Name
	}

	return ""
}

func buildPipeline(root config.Root, recipe config.Recipe, mappedChangelogConfig *config.ChangelogConfig) (pipeline.Pipeline, error) {
	if mappedChangelogConfig != nil && recipe.Type == changelogRecipeType {
		return changelogtask.NewFromConfig(changelogtask.Config(*mappedChangelogConfig)), nil
	}

	builder, ok := pipelineBuilders[recipe.Type]
	if !ok {
		return nil, fmt.Errorf("unknown recipe type: %s", recipe.Type)
	}

	pipelineInstance, err := builder(root, recipe)
	if err != nil {
		return nil, fmt.Errorf("build pipeline for recipe %s: %w", recipe.Name, err)
	}

	return pipelineInstance, nil
}

func buildSortPipeline(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error) {
	provider := sorttask.NewUnifiedProvider(root, recipe.Name)
	return sorttask.NewWithDeps(sorttask.DefaultFS(), provider), nil
}

func buildChangelogPipeline(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error) {
	mappedConfig, err := config.MapChangelog(recipe)
	if err != nil {
		return nil, fmt.Errorf("map changelog recipe %s: %w", recipe.Name, err)
	}
	return changelogtask.NewFromConfig(changelogtask.Config(mappedConfig)), nil
}
