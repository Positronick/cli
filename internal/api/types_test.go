package api

import (
	"encoding/json"
	"reflect"
	"strings"
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

// A meta-skill carries its bundled listing slugs in Listing.Data; the typed
// decode must round-trip them, and a wrong-typed field must fail loud rather
// than silently drop the dependency graph.
func TestListingSkillData(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		want    SkillData
		wantErr bool
	}{
		{
			name: "bundles decode field for field",
			data: map[string]any{"bundles": []any{"pr-to-green", "debugging"}},
			want: SkillData{Bundles: []string{"pr-to-green", "debugging"}},
		},
		{"empty data yields zero value", map[string]any{}, SkillData{}, false},
		{"nil data yields zero value", nil, SkillData{}, false},
		{"unknown keys ignored", map[string]any{"futureField": 1}, SkillData{}, false},
		{"wrong-typed bundles fails loud", map[string]any{"bundles": "pr-to-green"}, SkillData{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Listing{Data: tt.data}
			got, err := l.SkillData()
			if tt.wantErr {
				if err == nil {
					t.Fatal("SkillData should error on a wrong-typed field")
				}
				return
			}
			if err != nil {
				t.Fatalf("SkillData: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SkillData = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The CLI mirrors src/lib/types.ts field-for-field so an agent reading --json
// sees everything the server sends. These fields were added to the API after
// the CLI's first cut; this pins that they survive a decode→encode round-trip
// (i.e. are never silently dropped), including null for the nullable asset
// fields.
func TestNewWireFieldsRoundTrip(t *testing.T) {
	const listingJSON = `{"id":"01X","slug":"superpowers","profileHandle":"obra","profileName":"Jesse Vincent","profileTier":"verified","name":"Superpowers","type":"skill","tagline":"t","description":null,"category":"AI/ML","tags":[],"official":true,"sourceUrl":"https://e.x/s","repoUrl":null,"installCmd":null,"data":{},"hasAsset":true,"assetVersion":"1.0.0","assetContentHash":"abc","confidence":"official","status":"published","downloadCount":18,"chargeCount":6,"createdAt":"2026-04-01T09:00:00.000Z","updatedAt":"2026-04-02T09:00:00.000Z"}`
	var l Listing
	if err := json.Unmarshal([]byte(listingJSON), &l); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	if l.ProfileTier == nil || *l.ProfileTier != "verified" {
		t.Errorf("profileTier = %v, want verified", l.ProfileTier)
	}
	if !l.HasAsset || l.AssetVersion == nil || *l.AssetVersion != "1.0.0" {
		t.Errorf("asset fields lost: hasAsset=%v version=%v", l.HasAsset, l.AssetVersion)
	}
	if l.ChargeCount != 6 {
		t.Errorf("chargeCount = %d, want 6", l.ChargeCount)
	}
	out, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("encode listing: %v", err)
	}
	for _, want := range []string{`"profileTier":"verified"`, `"hasAsset":true`,
		`"assetVersion":"1.0.0"`, `"assetContentHash":"abc"`, `"chargeCount":6`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("re-encoded listing dropped %s\ngot: %s", want, out)
		}
	}

	// A null asset (non-skill listing) must round-trip as JSON null, not "".
	var bare Listing
	if err := json.Unmarshal([]byte(`{"assetVersion":null,"chargeCount":0}`), &bare); err != nil {
		t.Fatalf("decode bare: %v", err)
	}
	out, _ = json.Marshal(bare)
	if !strings.Contains(string(out), `"assetVersion":null`) {
		t.Errorf("nullable assetVersion must serialize as null, got: %s", out)
	}

	// SoulCard gained chargeCount alongside downloadCount.
	var s SoulCard
	if err := json.Unmarshal([]byte(`{"downloadCount":42,"chargeCount":5}`), &s); err != nil {
		t.Fatalf("decode soul: %v", err)
	}
	if s.ChargeCount != 5 {
		t.Errorf("SoulCard.ChargeCount = %d, want 5", s.ChargeCount)
	}
}
