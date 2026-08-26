package relay

import (
	"errors"
	"testing"
)

func TestChoosePlacementUsesHardRegionLatencyAndStableCapacityTieBreaks(t *testing.T) {
	request := PlacementRequest{AllowedRegions: []string{"eu-north"}, ParticipantIDs: []string{"player-a", "player-b"}, LatencyP95MS: 100}
	candidates := []Candidate{
		{ProviderID: "cncnet.baseline", RelayID: "relay-2", Region: "eu-west", State: Accepting, RemainingPacketRate: 999, ParticipantRTTMS: map[string]int{"player-a": 1, "player-b": 1}},
		{ProviderID: "native", RelayID: "relay-b", Region: "eu-north", State: Accepting, RemainingPacketRate: 900, RemainingEgressBytesPS: 10_000, ParticipantRTTMS: map[string]int{"player-a": 30, "player-b": 50}},
		{ProviderID: "native", RelayID: "relay-a", Region: "eu-north", State: Accepting, RemainingPacketRate: 900, RemainingEgressBytesPS: 10_000, ParticipantRTTMS: map[string]int{"player-a": 30, "player-b": 50}},
		{ProviderID: "native", RelayID: "relay-c", Region: "eu-north", State: Draining, RemainingPacketRate: 10_000, ParticipantRTTMS: map[string]int{"player-a": 2, "player-b": 2}},
	}
	decision, err := ChoosePlacement(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if decision.RelayID != "relay-a" {
		t.Fatalf("relay = %s, want stable relay-a tie-break", decision.RelayID)
	}
	if decision.MaximumParticipantRTTMS != 50 || decision.P95ParticipantRTTMS != 50 {
		t.Fatalf("latency decision = %+v", decision)
	}
	if decision.PolicyVersion != PlacementPolicyVersion {
		t.Fatalf("policy = %s", decision.PolicyVersion)
	}
	if len(decision.Inputs) != len(candidates) || decision.Inputs[0].ParticipantRTTMS["player-a"] != 1 {
		t.Fatal("decision did not retain non-secret candidate inputs")
	}
}

func TestChoosePlacementRejectsMissingOrOverLimitProbe(t *testing.T) {
	_, err := ChoosePlacement(PlacementRequest{AllowedRegions: []string{"eu-north"}, ParticipantIDs: []string{"player-a"}, LatencyP95MS: 10}, []Candidate{{ProviderID: "native", RelayID: "relay-a", Region: "eu-north", State: Accepting, ParticipantRTTMS: map[string]int{"player-a": 11}}})
	if !errors.Is(err, ErrNoRelayCapacity) {
		t.Fatalf("error = %v", err)
	}
}
