package sort

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/tyemirov/llm-tasks/internal/config"
)

const (
	sortGrantDownloadsDirectoryKey                  = "grant.base_directories.downloads"
	sortGrantStagingDirectoryKey                    = "grant.base_directories.staging"
	sortGrantDirectoryMissingEnvironmentErrorFormat = "resolve %s: missing environment variable(s): %s"
	sortGrantDirectoryBlankErrorFormat              = "resolve %s: expanded value is blank"
)

type environmentLookupFunc func(string) (string, bool)

var lookupEnvironmentVariable = os.LookupEnv

func resolveSortGrantBaseDirectories(source config.Sort, lookup environmentLookupFunc) (config.Sort, error) {
	resolved := source
	downloadsPath, downloadsError := resolveSortGrantDirectory(source.Grant.BaseDirectories.Downloads, sortGrantDownloadsDirectoryKey, lookup)
	if downloadsError != nil {
		return config.Sort{}, downloadsError
	}
	stagingPath, stagingError := resolveSortGrantDirectory(source.Grant.BaseDirectories.Staging, sortGrantStagingDirectoryKey, lookup)
	if stagingError != nil {
		return config.Sort{}, stagingError
	}
	resolved.Grant.BaseDirectories.Downloads = downloadsPath
	resolved.Grant.BaseDirectories.Staging = stagingPath
	return resolved, nil
}

func resolveSortGrantDirectory(rawValue string, configurationKey string, lookup environmentLookupFunc) (string, error) {
	expandedValue, missingVariables := expandEnvironmentVariables(rawValue, lookup)
	if len(missingVariables) > 0 {
		return "", fmt.Errorf(sortGrantDirectoryMissingEnvironmentErrorFormat, configurationKey, strings.Join(missingVariables, ", "))
	}
	if strings.TrimSpace(expandedValue) == "" {
		return "", fmt.Errorf(sortGrantDirectoryBlankErrorFormat, configurationKey)
	}
	return expandedValue, nil
}

func expandEnvironmentVariables(rawValue string, lookup environmentLookupFunc) (string, []string) {
	missingSet := make(map[string]struct{})
	expandedValue := os.Expand(rawValue, func(variableName string) string {
		variableValue, found := lookup(variableName)
		if !found {
			missingSet[variableName] = struct{}{}
			return ""
		}
		return variableValue
	})
	if len(missingSet) == 0 {
		return expandedValue, nil
	}
	missingVariables := make([]string, 0, len(missingSet))
	for variableName := range missingSet {
		missingVariables = append(missingVariables, variableName)
	}
	slices.Sort(missingVariables)
	return expandedValue, missingVariables
}
