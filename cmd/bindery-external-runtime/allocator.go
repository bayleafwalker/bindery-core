package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
)

// cncnetPrivateProviderID is the only transport an instrumented RA2/YR client
// may be placed on. Public CnCNet is not an acceptance environment, so the
// allocator refuses to serve anything else rather than falling back.
const cncnetPrivateProviderID = "cncnet-private"

const allocatorRepository = "https://github.com/bayleafwalker/bindery-core"

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// buildRevision is set from the image build. go run/local tests may supply
// BINDERY_BUILD_REVISION instead; a supplied value may not contradict an exact
// revision already embedded in the binary.
var buildRevision = "unknown"

type allocatorConfig struct {
	Provider      string
	Endpoint      string
	Region        string
	PolicyVersion string
	Revision      string
	ConfigDigest  string
}

// allocatorConfigFromEnv reads the deployment-owned placement policy. The
// endpoint must be supplied explicitly: defaulting it would let a
// misconfigured deployment quietly hand clients a relay that is not the one
// the acceptance packet claims.
func allocatorConfigFromEnv() (allocatorConfig, error) {
	config := allocatorConfig{
		Provider:      envOrDefault("BINDERY_RELAY_PROVIDER", cncnetPrivateProviderID),
		Endpoint:      os.Getenv("BINDERY_RELAY_ENDPOINT"),
		Region:        envOrDefault("BINDERY_RELAY_REGION", "eu-north"),
		PolicyVersion: envOrDefault("BINDERY_PLACEMENT_POLICY_VERSION", "cncnet-private-lab-v1"),
		Revision:      strings.TrimSpace(os.Getenv("BINDERY_BUILD_REVISION")),
	}
	if config.Revision == "" {
		config.Revision = buildRevision
	}
	if config.Provider != cncnetPrivateProviderID {
		return allocatorConfig{}, fmt.Errorf("this service serves only %q, not %q", cncnetPrivateProviderID, config.Provider)
	}
	if strings.TrimSpace(config.Endpoint) == "" {
		return allocatorConfig{}, errors.New("BINDERY_RELAY_ENDPOINT must name the private tunnel as host:port")
	}
	host, port, err := net.SplitHostPort(config.Endpoint)
	if err != nil || host == "" || port == "" {
		return allocatorConfig{}, fmt.Errorf("BINDERY_RELAY_ENDPOINT %q is not host:port", config.Endpoint)
	}
	if !revisionPattern.MatchString(config.Revision) {
		return allocatorConfig{}, errors.New("BINDERY_BUILD_REVISION must be the full 40-character Git commit")
	}
	if revisionPattern.MatchString(buildRevision) && config.Revision != buildRevision {
		return allocatorConfig{}, errors.New("BINDERY_BUILD_REVISION does not match the revision embedded in the binary")
	}
	encoded, err := json.Marshal(struct {
		Provider      string `json:"provider"`
		Endpoint      string `json:"endpoint"`
		Region        string `json:"region"`
		PolicyVersion string `json:"policy_version"`
	}{config.Provider, config.Endpoint, config.Region, config.PolicyVersion})
	if err != nil {
		return allocatorConfig{}, fmt.Errorf("encode allocator config: %w", err)
	}
	digest := sha256.Sum256(encoded)
	config.ConfigDigest = "sha256:" + hex.EncodeToString(digest[:])
	return config, nil
}

// newCncNetPrivateAllocator returns the placement seam implementation. Both
// clients of a session receive the same endpoint, which is what makes them
// meet on the private tunnel.
func newCncNetPrivateAllocator(config allocatorConfig) externalruntime.PlacementAllocator {
	return func(intent externalruntime.PlacementIntent) (externalruntime.PublicPlacement, error) {
		// An intent that cannot include the served region is a configuration
		// error, not something to satisfy with a different region.
		if len(intent.AllowedRegions) > 0 && !slices.Contains(intent.AllowedRegions, config.Region) {
			return externalruntime.PublicPlacement{}, fmt.Errorf("no %s capacity in regions %v", config.Region, intent.AllowedRegions)
		}
		allocationID, err := newUUIDv4()
		if err != nil {
			return externalruntime.PublicPlacement{}, err
		}
		return externalruntime.PublicPlacement{
			Region:            config.Region,
			RelayProviderID:   config.Provider,
			RelayAllocationID: allocationID,
			RelayEndpoint:     config.Endpoint,
			PolicyVersion:     config.PolicyVersion,
			DecisionSummary:   fmt.Sprintf("private CnCNet tunnel in %s; p95 intent %dms", config.Region, intent.LatencyP95MS),
			Allocator: externalruntime.ImplementationIdentity{
				Implementation: cncnetPrivateProviderID,
				Repository:     allocatorRepository,
				Revision:       config.Revision,
				ConfigDigest:   config.ConfigDigest,
			},
		}, nil
	}
}

func newUUIDv4() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
