package api

import (
	"reflect"
	"testing"
)

// `positronick loop show` exists to surface the loop recipe; Listing.Data is
// an untyped JSON object on the wire, so the typed decode must round-trip
// every LoopData field — including numbers, which arrive as float64.
func TestListingLoopData(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		want    LoopData
		wantErr bool
	}{
		{
			name: "full loop payload decodes field for field",
			data: map[string]any{
				"goal":            "PR approved and CI green",
				"checkCommand":    "gh pr checks",
				"exitCondition":   "all checks pass",
				"maxIterations":   float64(20),
				"compatibleTools": []any{"claude-code", "codex"},
				"kickoff":         "Run the loop on PR #42",
			},
			want: LoopData{
				Goal:            "PR approved and CI green",
				CheckCommand:    "gh pr checks",
				ExitCondition:   "all checks pass",
				MaxIterations:   20,
				CompatibleTools: []string{"claude-code", "codex"},
				Kickoff:         "Run the loop on PR #42",
			},
		},
		{
			name: "empty data object yields the zero value",
			data: map[string]any{},
			want: LoopData{},
		},
		{
			name: "nil data yields the zero value",
			data: nil,
			want: LoopData{},
		},
		{
			name: "unknown keys are ignored",
			data: map[string]any{"goal": "g", "futureField": true},
			want: LoopData{Goal: "g"},
		},
		{
			name:    "wrong-typed field fails loud instead of decoding garbage",
			data:    map[string]any{"maxIterations": "twenty"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Listing{Data: tt.data}
			got, err := l.LoopData()
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoopData should error on a wrong-typed field")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoopData: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoopData = %+v, want %+v", got, tt.want)
			}
		})
	}
}
