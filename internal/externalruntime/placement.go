package externalruntime

import (
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

// resolvePlacement is the mechanical implementation beneath the
// coordinator-frozen allocator seam. The request carries intent only; the
// allocator owns the public allocation identity and endpoint.
func resolvePlacement(allocator PlacementAllocator, intent PlacementIntent, sessionID string, now time.Time) (*PublicPlacement, error) {
	if allocator == nil {
		return nil, nil
	}
	placement, err := allocator(intent)
	if err != nil {
		return nil, err
	}
	placementID, err := newUUIDv7(now)
	if err != nil {
		return nil, err
	}
	placement.SchemaVersion = SchemaVersion
	placement.PlacementID = placementID
	placement.SessionID = sessionID
	placement.CreatedAt = now
	if err := validatePublicPlacement(placement); err != nil {
		return nil, err
	}
	return &placement, nil
}

func validatePublicPlacement(placement PublicPlacement) error {
	if placement.SchemaVersion != SchemaVersion ||
		blankOrControl(placement.PlacementID) || blankOrControl(placement.SessionID) || placement.CreatedAt.IsZero() ||
		blankOrControl(placement.Region) || len(placement.Region) > 64 ||
		blankOrControl(placement.RelayProviderID) || len(placement.RelayProviderID) > 128 ||
		blankOrControl(placement.PolicyVersion) || len(placement.PolicyVersion) > 64 ||
		len(placement.DecisionSummary) > 2048 || hasControl(placement.DecisionSummary) {
		return domainError("PLACEMENT_INVALID", "placement metadata is invalid")
	}
	if blankOrControl(placement.Allocator.Implementation) || len(placement.Allocator.Implementation) > 128 ||
		blankOrControl(placement.Allocator.Repository) || len(placement.Allocator.Repository) > 512 ||
		blankOrControl(placement.Allocator.Revision) || len(placement.Allocator.Revision) > 128 ||
		!hashPattern.MatchString(placement.Allocator.ConfigDigest) {
		return domainError("PLACEMENT_INVALID", "allocator implementation identity is invalid")
	}
	if _, err := relayv1.PeekMustUUID(placement.PlacementID); err != nil {
		return domainError("PLACEMENT_INVALID", "placement id must be a canonical UUID")
	}
	if _, err := relayv1.PeekMustUUID(placement.SessionID); err != nil {
		return domainError("PLACEMENT_INVALID", "placement session id must be a canonical UUID")
	}
	if _, err := relayv1.PeekMustUUID(placement.RelayAllocationID); err != nil {
		return domainError("PLACEMENT_INVALID", "relay allocation id must be a canonical UUID")
	}
	host, portText, err := net.SplitHostPort(placement.RelayEndpoint)
	if err != nil || host == "" {
		return domainError("PLACEMENT_INVALID", "relay endpoint must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || hasControl(placement.RelayEndpoint) || len(placement.RelayEndpoint) > 256 {
		return domainError("PLACEMENT_INVALID", "relay endpoint must contain a valid port")
	}
	return nil
}

func blankOrControl(value string) bool { return strings.TrimSpace(value) == "" || hasControl(value) }

func hasControl(value string) bool { return strings.IndexFunc(value, unicode.IsControl) >= 0 }
