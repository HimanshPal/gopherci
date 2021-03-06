package analyser

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/bradleyfalzon/gopherci/internal/db"
)

type mockAnalyser struct {
	Executed   [][]string
	ExecuteOut [][]byte
	ExecuteErr []error
	Stopped    bool
}

var _ Analyser = &mockAnalyser{}
var _ Executer = &mockAnalyser{}

func (a *mockAnalyser) NewExecuter(_ context.Context, _ string) (Executer, error) {
	// Return itself
	return a, nil
}

func (a *mockAnalyser) Execute(_ context.Context, args []string) (out []byte, err error) {
	a.Executed = append(a.Executed, args)
	out, a.ExecuteOut = a.ExecuteOut[0], a.ExecuteOut[1:]
	err, a.ExecuteErr = a.ExecuteErr[0], a.ExecuteErr[1:]
	return out, err
}

func (a *mockAnalyser) Stop(_ context.Context) error {
	a.Stopped = true
	return nil
}

func TestAnalyse_pr(t *testing.T) {
	cfg := Config{
		EventType: EventTypePullRequest,
		BaseURL:   "base-url",
		BaseRef:   "base-branch",
		HeadURL:   "head-url",
		HeadRef:   "head-branch",
	}

	tools := []db.Tool{
		{ID: 1, Name: "Name1", Path: "tool1", Args: "-flag %BASE_BRANCH% ./..."},
		{ID: 2, Name: "Name2", Path: "tool2"},
		{ID: 3, Name: "Name2", Path: "tool3"},
	}

	diff := []byte(`diff --git a/subdir/main.go b/subdir/main.go
new file mode 100644
index 0000000..6362395
--- /dev/null
+++ b/main.go
@@ -0,0 +1,1 @@
+var _ = fmt.Sprintln()`)

	analyser := &mockAnalyser{
		ExecuteOut: [][]byte{
			{},   // git clone
			{},   // git fetch
			diff, // git diff
			{},   // install-deps.sh
			[]byte(`/go/src/gopherci`),                   // pwd
			[]byte("main.go:1: error1"),                  // tool 1
			[]byte("file is not generated"),              // isFileGenerated
			[]byte("/go/src/gopherci/main.go:1: error2"), // tool 2 output abs paths
			[]byte("file is not generated"),              // isFileGenerated
			[]byte("main.go:1: error3"),                  // tool 3 tested a generated file
			[]byte("file is generated"),                  // isFileGenerated
		},
		ExecuteErr: []error{
			nil, // git clone
			nil, // git fetch
			nil, // git diff
			nil, // install-deps.sh
			nil, // pwd
			nil, // tool 1
			&NonZeroError{ExitCode: 1}, // isFileGenerated - not generated
			nil, // tool 2 output abs paths
			&NonZeroError{ExitCode: 1}, // isFileGenerated - not generated
			nil, // tool 3 tested a generated file
			nil, // isFileGenerated - generated
		},
	}

	mockDB := db.NewMockDB()
	analysis, _ := mockDB.StartAnalysis(1, 2)

	err := Analyse(context.Background(), analyser, tools, cfg, analysis)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := map[db.ToolID][]db.Issue{
		1: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name1: error1"}},
		2: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name2: error2"}},
		3: nil,
	}
	for toolID, issues := range want {
		if have := analysis.Tools[toolID].Issues; !reflect.DeepEqual(issues, have) {
			t.Errorf("unexpected issues for toolID %v\nwant: %+v\nhave: %+v", toolID, issues, have)
		}
	}
	if len(analysis.Tools) != len(want) {
		t.Errorf("analysis has %v tools want %v", len(analysis.Tools), len(want))
	}

	if !analyser.Stopped {
		t.Errorf("expected analyser to be stopped")
	}

	expectedArgs := [][]string{
		{"git", "clone", "--depth", "1", "--branch", cfg.HeadRef, "--single-branch", cfg.HeadURL, "."},
		{"git", "fetch", "--depth", "1", cfg.BaseURL, cfg.BaseRef},
		{"git", "diff", fmt.Sprintf("FETCH_HEAD...%v", cfg.HeadRef)},
		{"install-deps.sh"},
		{"pwd"},
		{"tool1", "-flag", "FETCH_HEAD", "./..."},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"tool2"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"tool3"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
	}

	if !reflect.DeepEqual(analyser.Executed, expectedArgs) {
		t.Errorf("\nhave %v\nwant %v", analyser.Executed, expectedArgs)
	}
}

func TestAnalyse_push(t *testing.T) {
	cfg := Config{
		EventType: EventTypePush,
		BaseURL:   "base-url",
		BaseRef:   "abcde~1",
		HeadURL:   "head-url",
		HeadRef:   "abcde",
	}

	tools := []db.Tool{
		{ID: 1, Name: "Name1", Path: "tool1", Args: "-flag %BASE_BRANCH% ./..."},
		{ID: 2, Name: "Name2", Path: "tool2"},
		{ID: 3, Name: "Name2", Path: "tool3"},
	}

	diff := []byte(`diff --git a/subdir/main.go b/subdir/main.go
new file mode 100644
index 0000000..6362395
--- /dev/null
+++ b/main.go
@@ -0,0 +1,1 @@
+var _ = fmt.Sprintln()`)

	analyser := &mockAnalyser{
		ExecuteOut: [][]byte{
			{},   // git clone
			{},   // git checkout
			diff, // git diff
			{},   // install-deps.sh
			[]byte(`/go/src/gopherci`),                   // pwd
			[]byte("main.go:1: error1"),                  // tool 1
			[]byte("file is not generated"),              // isFileGenerated
			[]byte("/go/src/gopherci/main.go:1: error2"), // tool 2 output abs paths
			[]byte("file is not generated"),              // isFileGenerated
			[]byte("main.go:1: error3"),                  // tool 3 tested a generated file
			[]byte("file is generated"),                  // isFileGenerated
		},
		ExecuteErr: []error{
			nil, // git clone
			nil, // git checkout
			nil, // git diff
			nil, // install-deps.sh
			nil, // pwd
			nil, // tool 1
			&NonZeroError{ExitCode: 1}, // isFileGenerated - not generated
			nil, // tool 2 output abs paths
			&NonZeroError{ExitCode: 1}, // isFileGenerated - not generated
			nil, // tool 3 tested a generated file
			nil, // isFileGenerated - generated
		},
	}

	mockDB := db.NewMockDB()
	analysis, _ := mockDB.StartAnalysis(1, 2)

	err := Analyse(context.Background(), analyser, tools, cfg, analysis)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := map[db.ToolID][]db.Issue{
		1: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name1: error1"}},
		2: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name2: error2"}},
		3: nil,
	}
	for toolID, issues := range want {
		if have := analysis.Tools[toolID].Issues; !reflect.DeepEqual(issues, have) {
			t.Errorf("unexpected issues for toolID %v\nwant: %+v\nhave: %+v", toolID, issues, have)
		}
	}
	if len(analysis.Tools) != len(want) {
		t.Errorf("analysis has %v tools want %v", len(analysis.Tools), len(want))
	}

	if !analyser.Stopped {
		t.Errorf("expected analyser to be stopped")
	}

	expectedArgs := [][]string{
		{"git", "clone", cfg.HeadURL, "."},
		{"git", "checkout", cfg.HeadRef},
		{"git", "diff", fmt.Sprintf("%v...%v", cfg.BaseRef, cfg.HeadRef)},
		{"install-deps.sh"},
		{"pwd"},
		{"tool1", "-flag", "abcde~1", "./..."},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"tool2"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"tool3"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
	}

	if !reflect.DeepEqual(analyser.Executed, expectedArgs) {
		t.Errorf("\nhave %v\nwant %v", analyser.Executed, expectedArgs)
	}
}

func TestAnalyse_unknown(t *testing.T) {
	cfg := Config{}
	analyser := &mockAnalyser{}

	mockDB := db.NewMockDB()
	analysis, _ := mockDB.StartAnalysis(1, 2)

	err := Analyse(context.Background(), analyser, nil, cfg, analysis)
	if err == nil {
		t.Fatal("expected error got nil")
	}
}

func TestGetPatch(t *testing.T) {
	wantPatch := []byte("git diff patch")

	analyser := &mockAnalyser{
		ExecuteOut: [][]byte{
			wantPatch,
		},
		ExecuteErr: []error{
			nil, // git diff
		},
	}

	patch, err := getPatch(context.Background(), analyser, "abcdef~1", "abcdef")
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	expectedArgs := [][]string{
		{"git", "diff", "abcdef~1...abcdef"},
	}

	if !reflect.DeepEqual(analyser.Executed, expectedArgs) {
		t.Errorf("\nhave %v\nwant %v", analyser.Executed, expectedArgs)
	}

	if !reflect.DeepEqual(patch, wantPatch) {
		t.Errorf("unexpected patch\nhave %v\nwant %v", patch, wantPatch)
	}
}

func TestGetPatch_diffError(t *testing.T) {
	wantPatch := []byte("git show patch")

	analyser := &mockAnalyser{
		ExecuteOut: [][]byte{
			[]byte("git diff output"),
			wantPatch,
		},
		ExecuteErr: []error{
			&NonZeroError{ExitCode: 128}, // git diff
			nil, // git show
		},
	}

	patch, err := getPatch(context.Background(), analyser, "abcdef~1", "abcdef")
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	expectedArgs := [][]string{
		{"git", "diff", "abcdef~1...abcdef"},
		{"git", "show", "abcdef"},
	}

	if !reflect.DeepEqual(analyser.Executed, expectedArgs) {
		t.Errorf("\nhave %v\nwant %v", analyser.Executed, expectedArgs)
	}

	if !reflect.DeepEqual(patch, wantPatch) {
		t.Errorf("unexpected patch\nhave %v\nwant %v", patch, wantPatch)
	}
}
