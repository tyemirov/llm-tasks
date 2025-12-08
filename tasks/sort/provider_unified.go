package sort

import (
	"fmt"

	"github.com/tyemirov/llm-tasks/internal/config"
)

type UnifiedSortConfigProvider struct {
	root   config.Root
	recipe config.Recipe
	loaded *config.SortYAML
}

func NewUnifiedProvider(root config.Root, recipeName string) SortConfigProvider {
	rx, ok := root.FindRecipe(recipeName)
	if !ok {
		panic(fmt.Errorf("sort provider: recipe %q not found", recipeName))
	}
	return &UnifiedSortConfigProvider{root: root, recipe: rx}
}

func (u *UnifiedSortConfigProvider) Load() (config.Sort, error) {
	// Map unified recipe body into a Sort config (legacy struct reused by task)
	sy, err := config.MapSort(u.recipe)
	if err != nil {
		return config.Sort{}, err
	}
	var out config.Sort
	out.Grant.BaseDirectories.Downloads = sy.Grant.BaseDirectories.Downloads
	out.Grant.BaseDirectories.Staging = sy.Grant.BaseDirectories.Staging
	out.Grant.Safety.DryRun = sy.Grant.Safety.DryRun
	for _, p := range sy.Projects {
		out.Projects = append(out.Projects, struct {
			Name     string   `yaml:"name"`
			Target   string   `yaml:"target"`
			Keywords []string `yaml:"keywords"`
		}{Name: p.Name, Target: p.Target, Keywords: p.Keywords})
	}
	out.Thresholds.MinConfidence = sy.Thresholds.MinConfidence
	resolvedSortConfiguration, resolutionError := resolveSortGrantBaseDirectories(out, lookupEnvironmentVariable)
	if resolutionError != nil {
		return config.Sort{}, resolutionError
	}
	return resolvedSortConfiguration, nil
}
