package relay

import "testing"

func TestCoordinatorFrozenPlacementFixtureCorpus(t *testing.T) {
	request := fixturePlacementRequest()
	candidates := fixturePlacementCandidates()
	if len(request.AllowedRegions) != 1 || request.AllowedRegions[0] != "eu-north" || request.LatencyP95MS != 100 {
		t.Fatalf("placement request = %+v", request)
	}
	if len(candidates) != 2 || candidates[0].RelayID != "relay-b" || candidates[1].RelayID != "relay-a" {
		t.Fatalf("placement candidates = %+v", candidates)
	}
	decision, err := ChoosePlacement(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if decision.RelayID != "relay-a" || decision.MaximumParticipantRTTMS != 50 || decision.P95ParticipantRTTMS != 50 {
		t.Fatalf("placement decision = %+v", decision)
	}
}
