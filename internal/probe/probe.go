// Package probe measures recall quality offline. For each sampled memory a
// local generative model writes questions the memory should answer; each
// question is then run through the ordinary recall path and the memory's rank
// is recorded.
package probe

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kilo666mj/wayminder/internal/memory"
)

// Generator writes questions that a memory should answer.
type Generator interface {
	Questions(ctx context.Context, item memory.Memory, count int) ([]string, error)
	Model() string
}

type Sampler interface {
	Sample(ctx context.Context, scope, seed string, limit int) ([]memory.Memory, error)
}

type Recaller interface {
	Recall(ctx context.Context, request memory.RecallRequest) ([]memory.Memory, error)
}

type Options struct {
	// Scope restricts the sample to one exact scope; empty samples all scopes.
	Scope string
	// Seed fixes which memories are sampled.
	Seed string
	// Memories is the sample size.
	Memories int
	// Questions is the number of questions generated per memory.
	Questions int
	// K is the recall limit; the agent-facing default is 5.
	K int
	// IncludeContent adds summaries and generated questions to the report.
	IncludeContent bool
	// Progress, when set, is called after each memory is probed.
	Progress func(done, total int)
}

type Metrics struct {
	Memories int     `json:"memories"`
	Probes   int     `json:"probes"`
	HitAt1   float64 `json:"hit_at_1"`
	HitAtK   float64 `json:"hit_at_k"`
	MRR      float64 `json:"mrr"`
}

type ScopeMetrics struct {
	Scope string `json:"scope"`
	Metrics
}

// ProbeResult is one generated question and where its memory ranked.
type ProbeResult struct {
	Question string `json:"question"`
	// Rank is 1-based; 0 means the memory was not in the top K.
	Rank int `json:"rank"`
}

// MemoryResult describes a memory that at least one of its own probes failed
// to return.
type MemoryResult struct {
	Memory memory.MemoryRef `json:"memory"`
	Probes int              `json:"probes"`
	Hits   int              `json:"hits"`
	// BestRank is the best rank over all probes; 0 means never returned.
	BestRank  int           `json:"best_rank"`
	Questions []ProbeResult `json:"questions,omitempty"`
}

type Report struct {
	GeneratedAt        time.Time      `json:"generated_at"`
	GeneratorModel     string         `json:"generator_model"`
	Seed               string         `json:"seed"`
	Scope              string         `json:"scope,omitempty"`
	K                  int            `json:"k"`
	Overall            Metrics        `json:"overall"`
	GenerationFailures int            `json:"generation_failures"`
	ByScope            []ScopeMetrics `json:"by_scope"`
	// Weak lists memories with at least one missed probe, worst first.
	Weak []MemoryResult `json:"weak"`
}

type tally struct {
	memories, probes, hitsAt1, hitsAtK int
	reciprocal                         float64
}

func (t *tally) add(rank int) {
	t.probes++
	if rank == 0 {
		return
	}
	t.hitsAtK++
	t.reciprocal += 1 / float64(rank)
	if rank == 1 {
		t.hitsAt1++
	}
}

func (t *tally) metrics() Metrics {
	result := Metrics{Memories: t.memories, Probes: t.probes}
	if t.probes > 0 {
		probes := float64(t.probes)
		result.HitAt1 = float64(t.hitsAt1) / probes
		result.HitAtK = float64(t.hitsAtK) / probes
		result.MRR = t.reciprocal / probes
	}
	return result
}

// Run samples memories, generates questions for each, and scores recall.
// A memory whose questions cannot be generated is counted and skipped; a
// recall failure aborts the run because every later result would be suspect.
func Run(ctx context.Context, sampler Sampler, generator Generator, recaller Recaller, options Options) (Report, error) {
	if options.Memories < 1 || options.Questions < 1 || options.K < 1 {
		return Report{}, errors.New("memories, questions, and k must each be at least 1")
	}
	items, err := sampler.Sample(ctx, options.Scope, options.Seed, options.Memories)
	if err != nil {
		return Report{}, fmt.Errorf("sample memories: %w", err)
	}
	if len(items) == 0 {
		return Report{}, errors.New("no live memories matched the sample")
	}

	report := Report{
		GeneratedAt: time.Now().UTC(), GeneratorModel: generator.Model(),
		Seed: options.Seed, Scope: options.Scope, K: options.K,
		ByScope: []ScopeMetrics{}, Weak: []MemoryResult{},
	}
	var overall tally
	scopes := map[string]*tally{}
	var lastGenerationErr error

	for index, item := range items {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		questions, err := generator.Questions(ctx, item, options.Questions)
		if err == nil && len(questions) == 0 {
			err = errors.New("generator returned no questions")
		}
		if err != nil {
			if ctx.Err() != nil {
				return Report{}, ctx.Err()
			}
			report.GenerationFailures++
			lastGenerationErr = err
			if options.Progress != nil {
				options.Progress(index+1, len(items))
			}
			continue
		}

		scope, ok := scopes[item.Scope]
		if !ok {
			scope = &tally{}
			scopes[item.Scope] = scope
		}
		overall.memories++
		scope.memories++
		result := MemoryResult{Memory: memory.NewMemoryRef(item, false), Probes: len(questions)}
		if options.IncludeContent {
			result.Memory.Summary = item.Summary
		}
		for _, question := range questions {
			recalled, err := recaller.Recall(ctx, memory.RecallRequest{Query: question, Scope: item.Scope, Limit: options.K})
			if err != nil {
				return Report{}, fmt.Errorf("recall probe for memory %s: %w", item.ID, err)
			}
			rank := rankOf(recalled, item.ID)
			overall.add(rank)
			scope.add(rank)
			if rank > 0 {
				result.Hits++
				if result.BestRank == 0 || rank < result.BestRank {
					result.BestRank = rank
				}
			}
			if options.IncludeContent {
				result.Questions = append(result.Questions, ProbeResult{Question: question, Rank: rank})
			}
		}
		if result.Hits < result.Probes {
			report.Weak = append(report.Weak, result)
		}
		if options.Progress != nil {
			options.Progress(index+1, len(items))
		}
	}

	if overall.memories == 0 {
		return Report{}, fmt.Errorf("question generation failed for all %d sampled memories: %w", len(items), lastGenerationErr)
	}
	report.Overall = overall.metrics()
	for name, scope := range scopes {
		report.ByScope = append(report.ByScope, ScopeMetrics{Scope: name, Metrics: scope.metrics()})
	}
	sort.Slice(report.ByScope, func(i, j int) bool { return report.ByScope[i].Scope < report.ByScope[j].Scope })
	sort.SliceStable(report.Weak, func(i, j int) bool {
		a, b := report.Weak[i], report.Weak[j]
		if a.Hits*b.Probes != b.Hits*a.Probes {
			return a.Hits*b.Probes < b.Hits*a.Probes
		}
		return a.Memory.ID < b.Memory.ID
	})
	return report, nil
}

func rankOf(items []memory.Memory, id string) int {
	for index, item := range items {
		if item.ID == id {
			return index + 1
		}
	}
	return 0
}

// cleanQuestions trims, de-duplicates, and bounds generated questions.
func cleanQuestions(questions []string, count int) []string {
	seen := map[string]bool{}
	result := make([]string, 0, count)
	for _, question := range questions {
		question = strings.Join(strings.Fields(question), " ")
		key := strings.ToLower(question)
		if question == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, question)
		if len(result) == count {
			break
		}
	}
	return result
}
