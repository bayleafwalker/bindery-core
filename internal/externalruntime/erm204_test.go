package externalruntime

import (
	"encoding/json"
	"testing"
)

const erm204AllocationID = "0198c2c3-4d5e-7f60-8123-456789abcdef"

func TestERM204PublishesCoordinatorPlacement(t *testing.T) {
	var received PlacementIntent
	service := NewServiceWithPlacementAllocator(func(intent PlacementIntent) (PublicPlacement, error) {
		received = intent
		return PublicPlacement{
			Region:            "eu-north",
			RelayProviderID:   "bindery-native",
			RelayAllocationID: erm204AllocationID,
			RelayEndpoint:     "127.0.0.1:40000",
			PolicyVersion:     "relay-placement/v1",
			DecisionSummary:   "lowest-p95 eligible relay",
			Allocator:         fixtureAllocatorIdentity(),
		}, nil
	})
	identity := mustIdentity(t, service, "erm204-owner")
	request := testSessionRequest()
	created, err := service.CreateSession(identity.AccountToken, "erm204-session", request)
	if err != nil {
		t.Fatal(err)
	}
	if received.LatencyP95MS != request.Placement.LatencyP95MS || len(received.AllowedRegions) != 1 || received.AllowedRegions[0] != "eu-north" {
		t.Fatalf("allocator intent = %+v, want %+v", received, request.Placement)
	}
	placement := created.PublicSession.Placement
	if placement == nil || placement.PlacementID == "" || placement.SessionID != created.PublicSession.SessionID || placement.RelayAllocationID != erm204AllocationID || placement.RelayEndpoint != "127.0.0.1:40000" || placement.PolicyVersion != "relay-placement/v1" || placement.Allocator.Revision == "" {
		t.Fatalf("public placement = %+v", placement)
	}
	resolved, err := service.GetPlacement(placement.PlacementID)
	if err != nil || resolved.PlacementID != created.PublicSession.PlacementID {
		t.Fatalf("stable placement did not resolve: %+v, error = %v", resolved, err)
	}
	execution, err := service.GetExecution(created.PublicSession.ExecutionID)
	if err != nil || execution.SessionID != created.PublicSession.SessionID || execution.PlacementID != placement.PlacementID {
		t.Fatalf("stable execution did not resolve: %+v, error = %v", execution, err)
	}
	encoded, err := json.Marshal(created.PublicSession)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 || string(encoded) == "null" {
		t.Fatal("public session placement was not serializable")
	}
}

func TestERM204RejectsInvalidCoordinatorPlacementBeforeSession(t *testing.T) {
	cases := []struct {
		name      string
		placement PublicPlacement
	}{
		{name: "missing provider", placement: PublicPlacement{Region: "eu-north", RelayAllocationID: erm204AllocationID, RelayEndpoint: "127.0.0.1:40000", PolicyVersion: "relay-placement/v1", Allocator: fixtureAllocatorIdentity()}},
		{name: "invalid allocation", placement: PublicPlacement{Region: "eu-north", RelayProviderID: "bindery-native", RelayAllocationID: "not-a-uuid", RelayEndpoint: "127.0.0.1:40000", PolicyVersion: "relay-placement/v1", Allocator: fixtureAllocatorIdentity()}},
		{name: "invalid endpoint", placement: PublicPlacement{Region: "eu-north", RelayProviderID: "bindery-native", RelayAllocationID: erm204AllocationID, RelayEndpoint: "not-an-endpoint", PolicyVersion: "relay-placement/v1", Allocator: fixtureAllocatorIdentity()}},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := NewServiceWithPlacementAllocator(func(PlacementIntent) (PublicPlacement, error) { return test.placement, nil })
			identity := mustIdentity(t, service, "erm204-invalid-"+string(rune('a'+index)))
			if _, err := service.CreateSession(identity.AccountToken, "erm204-invalid-"+test.name, testSessionRequest()); !hasCode(err, "PLACEMENT_INVALID") {
				t.Fatalf("create error = %v, want PLACEMENT_INVALID", err)
			}
			if len(service.sessions) != 0 {
				t.Fatalf("invalid placement created %d sessions", len(service.sessions))
			}
		})
	}
}
