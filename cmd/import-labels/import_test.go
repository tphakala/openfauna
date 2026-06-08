package main

import "testing"

func TestMergeLabels_DeltaMode(t *testing.T) {
	master := map[string]string{
		"Pica pica":        "Eurasian Magpie",
		"Tachyspiza badia": "Shikra",
		"Turdus merula":    "Eurasian Blackbird",
	}
	aliases := map[string]string{"Accipiter badius": "Tachyspiza badia"}
	existing := map[string]string{"NOISE": "noise"} // pre-existing non-species entry must survive
	lines := []string{
		"Pica pica_Common Magpie",          // differs from master -> add British override
		"Accipiter badius_Shikra",          // alias -> Tachyspiza badia, equals master -> skip
		"Turdus merula_Eurasian Blackbird", // equals master -> skip
		"Human_Human",                      // not in master -> skip
		"",                                 // blank -> skip
	}

	res := mergeLabels(existing, master, aliases, lines, true)

	if res.added != 1 {
		t.Fatalf("added = %d, want 1", res.added)
	}
	if got := res.merged["Pica pica"]; got != "Common Magpie" {
		t.Errorf("Pica pica = %q, want Common Magpie", got)
	}
	if _, ok := res.merged["Tachyspiza badia"]; ok {
		t.Errorf("Tachyspiza badia must not be added in delta mode (equals master)")
	}
	if got := res.merged["NOISE"]; got != "noise" {
		t.Errorf("existing NOISE entry not preserved: %q", got)
	}
}

func TestMergeLabels_FullMode(t *testing.T) {
	master := map[string]string{"Pica pica": "Eurasian Magpie", "Turdus merula": "Eurasian Blackbird"}
	lines := []string{"Pica pica_Skata", "Turdus merula_Mustarastas", "Human_Ihminen"}

	res := mergeLabels(map[string]string{}, master, map[string]string{}, lines, false)

	if res.added != 2 {
		t.Fatalf("added = %d, want 2 (Human skipped: not in master)", res.added)
	}
	if res.merged["Turdus merula"] != "Mustarastas" {
		t.Errorf("Turdus merula = %q, want Mustarastas", res.merged["Turdus merula"])
	}
}

func TestVerifyNoRegression(t *testing.T) {
	master := map[string]string{"Pica pica": "Eurasian Magpie", "Turdus merula": "Eurasian Blackbird"}
	lines := []string{"Pica pica_Common Magpie", "Turdus merula_Eurasian Blackbird"}

	// No override yet: Pica pica would show the American base -> one mismatch.
	if miss := verifyNoRegression(map[string]string{}, master, map[string]string{}, lines); len(miss) != 1 {
		t.Fatalf("before override: got %d mismatches, want 1", len(miss))
	}
	// Override layered on: no regression.
	target := map[string]string{"Pica pica": "Common Magpie"}
	if miss := verifyNoRegression(target, master, map[string]string{}, lines); len(miss) != 0 {
		t.Fatalf("after override: got %d mismatches, want 0", len(miss))
	}
}
