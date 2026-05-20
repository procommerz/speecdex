package search

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type yamlOutput struct {
	Results       []yamlResult `yaml:"results"`
	IndexedBranch string       `yaml:"indexed_branch"`
}

type yamlResult struct {
	File         string   `yaml:"file"`
	StartLine    int      `yaml:"start_line"`
	EndLine      int      `yaml:"end_line"`
	Score        float64  `yaml:"score"`
	MatchSources []string `yaml:"match_sources"`
	MatchedText  []string `yaml:"matched_text,omitempty"`
	Text         string   `yaml:"text"`
}

func WriteYAML(w io.Writer, results []Result, indexedBranch string) error {
	if _, err := fmt.Fprintln(w, "```yaml"); err != nil {
		return err
	}
	out := yamlOutput{
		Results:       make([]yamlResult, len(results)),
		IndexedBranch: indexedBranch,
	}
	for i, result := range results {
		out.Results[i] = yamlResult{
			File:         result.File,
			StartLine:    result.StartLine,
			EndLine:      result.EndLine,
			Score:        result.Score,
			MatchSources: result.MatchSources,
			MatchedText:  result.MatchedText,
			Text:         result.Text,
		}
	}

	encoded, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "```")
	return err
}
