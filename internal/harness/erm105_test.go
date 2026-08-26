package harness

import "testing"

func TestERM105SyntheticHarnessReproducesRequiredScenarios(t *testing.T) {
	fixture := fixtureTwoClientHarness()
	results := New(newSyntheticRelayProvider(fixture)).Run()
	if len(results) != len(Scenarios) {
		t.Fatalf("result count = %d, want %d", len(results), len(Scenarios))
	}
	required := map[Scenario]bool{
		BidirectionalTraffic: true,
		UnauthorizedSource:   true,
		Drain:                true,
		ForcedLoss:           true,
	}
	for _, result := range results {
		if required[result.Scenario] && !result.Passed {
			t.Fatalf("required scenario failed: %+v", result)
		}
	}
}
