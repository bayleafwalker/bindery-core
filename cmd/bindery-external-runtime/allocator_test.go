package main

import (
	"os"
	"testing"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

func TestAllocatorConfigRequiresAnEndpoint(t *testing.T) {
	t.Setenv("BINDERY_RELAY_ENDPOINT", "")
	if _, err := allocatorConfigFromEnv(); err == nil {
		t.Fatal("expected a missing endpoint to be refused")
	}
}

func TestAllocatorConfigRejectsOtherProviders(t *testing.T) {
	t.Setenv("BINDERY_RELAY_ENDPOINT", "192.168.122.1:50001")
	t.Setenv("BINDERY_RELAY_PROVIDER", "cncnet-public")
	if _, err := allocatorConfigFromEnv(); err == nil {
		t.Fatal("expected a non-private provider to be refused")
	}
}

func TestAllocatorConfigRejectsMalformedEndpoints(t *testing.T) {
	t.Setenv("BINDERY_RELAY_ENDPOINT", "192.168.122.1")
	if _, err := allocatorConfigFromEnv(); err == nil {
		t.Fatal("expected an endpoint without a port to be refused")
	}
}

func TestBothClientsReceiveTheSameEndpointWithDistinctAllocations(t *testing.T) {
	os.Setenv("BINDERY_RELAY_ENDPOINT", "192.168.122.1:50001")
	defer os.Unsetenv("BINDERY_RELAY_ENDPOINT")
	config, err := allocatorConfigFromEnv()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	allocate := newCncNetPrivateAllocator(config)
	intent := externalruntime.PlacementIntent{AllowedRegions: []string{"eu-north"}, LatencyP95MS: 100}

	first, err := allocate(intent)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := allocate(intent)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if first.RelayEndpoint != second.RelayEndpoint {
		t.Fatalf("clients must meet on one endpoint: %q vs %q", first.RelayEndpoint, second.RelayEndpoint)
	}
	if first.RelayProviderID != "cncnet-private" {
		t.Fatalf("unexpected provider %q", first.RelayProviderID)
	}
	if first.RelayAllocationID == second.RelayAllocationID {
		t.Fatal("each allocation needs its own identity")
	}
	if _, err := relayv1.PeekMustUUID(first.RelayAllocationID); err != nil {
		t.Fatalf("allocation id must be a canonical UUID: %v", err)
	}
}

func TestIntentWithoutTheServedRegionIsRefused(t *testing.T) {
	t.Setenv("BINDERY_RELAY_ENDPOINT", "192.168.122.1:50001")
	config, err := allocatorConfigFromEnv()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	allocate := newCncNetPrivateAllocator(config)

	if _, err := allocate(externalruntime.PlacementIntent{AllowedRegions: []string{"us-west"}, LatencyP95MS: 100}); err == nil {
		t.Fatal("expected an unservable region to be refused rather than silently reassigned")
	}
}
