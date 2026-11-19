package probe

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kilo666mj/wayminder/internal/memory"
)

type fakeSampler struct {
	items []memory.Memory
	scope string
	seed  string
	limit int
}

func (f *fakeSampler) Sample(_ context.Context, scope, seed string, limit int) ([]memory.Memory, error) {
	f.scope, f.seed, f.limit = scope, seed, limit
	return f.items, nil
}

type fakeGenerator struct {
	questions map[string][]string
	fail      map[string]bool
}

func (fakeGenerator) Model() string { return "fake-generator" }
func (f fakeGenerator) Questions(_ context.Context, item memory.Memory, _ int) ([]string, error) {
	if f.fail[item.ID] {
		return nil, errors.New("generation failed")
	}
	return f.questions[item.ID], nil
}

type fakeRecaller struct {
	results  map[string][]string
	requests []memory.RecallRequest
	err      error
}

func (f *fakeRecaller) Recall(_ context.Context, request memory.RecallRequest) ([]memory.Memory, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return nil, f.err
	}
	var items []memory.Memory
	for _, id := range f.results[request.Query] {
		items = append(items, memory.Memory{ID: id})
	}
	return items, nil
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestRunScoresRanksAndListsWeakMemories(t *testing.T) {
	sampler := &fakeSampler{items: []memory.Memory{
		{ID: "a", Scope: "repo:example", Kind: "fact", Summary: "summary a"},
		{ID: "b", Scope: "host:example", Kind: "fact", Summary: "summary b"},
	}}
	generator := fakeGenerator{questions: map[string][]string{
		"a": {"a first", "a second"},
		"b": {"b first", "b missing"},
	}}
	recaller := &fakeRecaller{results: map[string][]string{
		"a first":   {"a", "x"},
		"a second":  {"x", "a"},
		"b first":   {"x", "y", "z", "b"},
		"b missing": {"x", "y"},
	}}
	var progress []int
	report, err := Run(context.Background(), sampler, generator, recaller, Options{
		Seed: "seed", Memories: 2, Questions: 2, K: 5,
		Progress: func(done, _ int) { progress = append(progress, done) },
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	overall := report.Overall
	if overall.Memories != 2 || overall.Probes != 4 || !approx(overall.HitAt1, 0.25) || !approx(overall.HitAtK, 0.75) || !approx(overall.MRR, (1+0.5+0.25)/4) {
		t.Fatalf("unexpected overall metrics: %#v", overall)
	}
	if len(report.ByScope) != 2 || report.ByScope[0].Scope != "host:example" || !approx(report.ByScope[0].HitAtK, 0.5) || !approx(report.ByScope[1].HitAtK, 1) {
		t.Fatalf("unexpected scope metrics: %#v", report.ByScope)
	}
	if len(report.Weak) != 1 || report.Weak[0].Memory.ID != "b" || report.Weak[0].Hits != 1 || report.Weak[0].BestRank != 4 {
		t.Fatalf("unexpected weak memories: %#v", report.Weak)
	}
	if report.Weak[0].Memory.Summary != "" || report.Weak[0].Questions != nil {
		t.Fatalf("report leaked content without -content: %#v", report.Weak[0])
	}
	if !reflect.DeepEqual(progress, []int{1, 2}) || sampler.seed != "seed" || sampler.limit != 2 {
		t.Fatalf("progress=%v sampler=%#v", progress, sampler)
	}
	for _, request := range recaller.requests {
		if request.Limit != 5 || (request.Scope != "repo:example" && request.Scope != "host:example") {
			t.Fatalf("recall did not use the memory's scope and k: %#v", request)
		}
	}
}

func TestRunIncludesQuestionsWhenRequested(t *testing.T) {
	sampler := &fakeSampler{items: []memory.Memory{{ID: "a", Scope: "global", Summary: "summary a", Content: "content a"}}}
	generator := fakeGenerator{questions: map[string][]string{"a": {"missing"}}}
	report, err := Run(context.Background(), sampler, generator, &fakeRecaller{}, Options{Memories: 1, Questions: 1, K: 5, IncludeContent: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	weak := report.Weak[0]
	if weak.Memory.Summary != "summary a" || weak.Memory.Content != "" {
		t.Fatalf("unexpected memory reference: %#v", weak.Memory)
	}
	if !reflect.DeepEqual(weak.Questions, []ProbeResult{{Question: "missing", Rank: 0}}) || weak.BestRank != 0 {
		t.Fatalf("unexpected questions: %#v", weak)
	}
}

func TestRunCountsGenerationFailuresAndContinues(t *testing.T) {
	sampler := &fakeSampler{items: []memory.Memory{{ID: "a", Scope: "global"}, {ID: "b", Scope: "global"}, {ID: "c", Scope: "global"}}}
	generator := fakeGenerator{
		questions: map[string][]string{"b": {"b first"}},
		fail:      map[string]bool{"a": true},
	}
	recaller := &fakeRecaller{results: map[string][]string{"b first": {"b"}}}
	report, err := Run(context.Background(), sampler, generator, recaller, Options{Memories: 3, Questions: 1, K: 5})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// "a" fails outright and "c" yields no questions.
	if report.GenerationFailures != 2 || report.Overall.Memories != 1 || !approx(report.Overall.HitAt1, 1) || len(report.Weak) != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRunFailsWhenNothingCanBeProbed(t *testing.T) {
	sampler := &fakeSampler{items: []memory.Memory{{ID: "a", Scope: "global"}}}
	if _, err := Run(context.Background(), sampler, fakeGenerator{fail: map[string]bool{"a": true}}, &fakeRecaller{}, Options{Memories: 1, Questions: 1, K: 5}); err == nil {
		t.Fatal("Run() succeeded although every generation failed")
	}
	if _, err := Run(context.Background(), &fakeSampler{}, fakeGenerator{}, &fakeRecaller{}, Options{Memories: 1, Questions: 1, K: 5}); err == nil {
		t.Fatal("Run() succeeded with an empty sample")
	}
	if _, err := Run(context.Background(), sampler, fakeGenerator{}, &fakeRecaller{}, Options{Memories: 1, Questions: 0, K: 5}); err == nil {
		t.Fatal("Run() accepted zero questions")
	}
}

func TestRunAbortsOnRecallFailure(t *testing.T) {
	sampler := &fakeSampler{items: []memory.Memory{{ID: "a", Scope: "global"}}}
	generator := fakeGenerator{questions: map[string][]string{"a": {"question"}}}
	recallErr := errors.New("embedding provider down")
	_, err := Run(context.Background(), sampler, generator, &fakeRecaller{err: recallErr}, Options{Memories: 1, Questions: 1, K: 5})
	if !errors.Is(err, recallErr) {
		t.Fatalf("Run() error = %v, want recall failure", err)
	}
}

func TestOllamaQuestionsRequestsStructuredOutput(t *testing.T) {
	var received struct {
		Model    string         `json:"model"`
		Stream   bool           `json:"stream"`
		Format   map[string]any `json:"format"`
		Messages []chatMessage  `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		content := `{"questions":["  Where does   Postgres run? ","where does postgres run?","","Which host has the database?","A third?"]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": content}})
	}))
	defer server.Close()

	generator := NewOllama(server.URL+"/", "generator", time.Second)
	questions, err := generator.Questions(context.Background(), memory.Memory{Content: "Postgres runs on app-host", Scope: "host:app-host", Kind: "fact"}, 2)
	if err != nil {
		t.Fatalf("Questions() error = %v", err)
	}
	if !reflect.DeepEqual(questions, []string{"Where does Postgres run?", "Which host has the database?"}) {
		t.Fatalf("Questions() = %q", questions)
	}
	if received.Model != "generator" || received.Stream || received.Format["type"] != "object" || len(received.Messages) != 2 {
		t.Fatalf("unexpected request: %#v", received)
	}
	if !strings.Contains(received.Messages[1].Content, "Postgres runs on app-host") {
		t.Fatalf("note missing from prompt: %q", received.Messages[1].Content)
	}
}

func TestOllamaQuestionsDoesNotEchoModelOutputInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": "the secret note text"}})
	}))
	defer server.Close()
	_, err := NewOllama(server.URL, "generator", time.Second).Questions(context.Background(), memory.Memory{Content: "note"}, 1)
	if err == nil || strings.Contains(err.Error(), "secret note text") {
		t.Fatalf("Questions() error = %v", err)
	}
}

func TestOllamaQuestionsReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer server.Close()
	_, err := NewOllama(server.URL, "missing", time.Second).Questions(context.Background(), memory.Memory{Content: "note"}, 1)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("Questions() error = %v", err)
	}
}
