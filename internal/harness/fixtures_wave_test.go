package harness

import "testing"

func TestCoordinatorFrozenScenarioFixtureCorpus(t *testing.T) {
	results := fixtureScenarioResults("fixture provider")
	if len(results) != len(Scenarios) {
		t.Fatalf("scenario fixture count = %d, want %d", len(results), len(Scenarios))
	}
	for _, scenario := range Scenarios {
		result, ok := results[scenario]
		if !ok || !result.Passed || result.Observed != "fixture provider" {
			t.Fatalf("scenario %q fixture = %+v, present=%t", scenario, result, ok)
		}
	}
}
