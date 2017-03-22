	"context"
	ExecuteErr []error
func (a *mockAnalyser) NewExecuter(_ context.Context, _ string) (Executer, error) {
func (a *mockAnalyser) Execute(_ context.Context, args []string) (out []byte, err error) {
	err, a.ExecuteErr = a.ExecuteErr[0], a.ExecuteErr[1:]
func (a *mockAnalyser) Stop(_ context.Context) error {
		{ID: 1, Name: "Name1", Path: "tool1", Args: "-flag %BASE_BRANCH% ./..."},
		{ID: 2, Name: "Name2", Path: "tool2"},
		{ID: 3, Name: "Name2", Path: "tool3"},
			[]byte("file is not generated"),              // isFileGenerated
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
	analysis, err := Analyse(context.Background(), analyser, tools, cfg)
	want := map[db.ToolID][]db.Issue{
		1: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name1: error1"}},
		2: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name2: error2"}},
		3: nil,
	}
	for toolID, issues := range want {
		if have := analysis.Tools[toolID].Issues; !reflect.DeepEqual(issues, have) {
			t.Errorf("unexpected issues for toolID %v\nwant: %+v\nhave: %+v", toolID, issues, have)
		}
	if len(analysis.Tools) != len(want) {
		t.Errorf("analysis has %v tools want %v", len(analysis.Tools), len(want))
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"tool3"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{ID: 1, Name: "Name1", Path: "tool1", Args: "-flag %BASE_BRANCH% ./..."},
		{ID: 2, Name: "Name2", Path: "tool2"},
		{ID: 3, Name: "Name2", Path: "tool3"},
			[]byte("file is not generated"),              // isFileGenerated
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
	analysis, err := Analyse(context.Background(), analyser, tools, cfg)
	want := map[db.ToolID][]db.Issue{
		1: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name1: error1"}},
		2: []db.Issue{{Path: "main.go", Line: 1, HunkPos: 1, Issue: "Name2: error2"}},
		3: nil,
	}
	for toolID, issues := range want {
		if have := analysis.Tools[toolID].Issues; !reflect.DeepEqual(issues, have) {
			t.Errorf("unexpected issues for toolID %v\nwant: %+v\nhave: %+v", toolID, issues, have)
		}
	if len(analysis.Tools) != len(want) {
		t.Errorf("analysis has %v tools want %v", len(analysis.Tools), len(want))
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
		{"tool3"},
		{"isFileGenerated", "/go/src/gopherci", "main.go"},
	_, err := Analyse(context.Background(), analyser, nil, cfg)