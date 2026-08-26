package harness

import "testing"

func TestDualProviderHarnessRunsIdenticalScenarioSet(t *testing.T) {
	native := RecordedProvider{ProviderName: "bindery.native-udp-relay", Outcomes: allPassing("native forwarding")}
	baselineOutcomes := allPassing("baseline tunnel forwarding")
	baselineOutcomes[UnauthorizedSource] = Result{Passed: true, Observed: "source limitation measured; provider has no Bindery per-client transport key", Limitations: []string{"no-per-client-hmac"}}
	baselineOutcomes[TelemetrySinkInterrupt] = Result{Passed: true, Observed: "telemetry coupling is not independently controllable in unchanged baseline", Limitations: []string{"telemetry-sink-not-separable"}}
	baseline := RecordedProvider{ProviderName: "cncnet.baseline", Outcomes: baselineOutcomes}
	results := New(native, baseline).Run()
	if len(results) != len(Scenarios)*2 {
		t.Fatalf("result count = %d", len(results))
	}
	for offset := 0; offset < len(Scenarios); offset++ {
		if results[offset].Provider != "bindery.native-udp-relay" || results[offset].Scenario != Scenarios[offset] {
			t.Fatalf("native result order = %+v", results[offset])
		}
	}
	for offset := len(Scenarios); offset < len(results); offset++ {
		if results[offset].Provider != "cncnet.baseline" {
			t.Fatalf("baseline result order = %+v", results[offset])
		}
	}
	if len(baselineOutcomes[UnauthorizedSource].Limitations) == 0 {
		t.Fatal("baseline limitation was hidden")
	}
}

func allPassing(observed string) map[Scenario]Result {
	result := make(map[Scenario]Result, len(Scenarios))
	for _, scenario := range Scenarios {
		result[scenario] = Result{Passed: true, Observed: observed}
	}
	return result
}
