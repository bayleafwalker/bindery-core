package harness

import "sort"

type Scenario string

const (
	BidirectionalTraffic   Scenario = "bidirectional-traffic"
	UnauthorizedSource     Scenario = "unauthorized-source"
	MalformedPacket        Scenario = "malformed-packet"
	OversizedPacket        Scenario = "oversized-packet"
	RateLimitedPacket      Scenario = "rate-limited-packet"
	LeaseExpiry            Scenario = "lease-expiry"
	Drain                  Scenario = "drain"
	ForcedLoss             Scenario = "forced-loss"
	TelemetrySinkInterrupt Scenario = "telemetry-sink-interruption"
)

var Scenarios = []Scenario{BidirectionalTraffic, UnauthorizedSource, MalformedPacket, OversizedPacket, RateLimitedPacket, LeaseExpiry, Drain, ForcedLoss, TelemetrySinkInterrupt}

type Result struct {
	Provider    string   `json:"provider"`
	Scenario    Scenario `json:"scenario"`
	Passed      bool     `json:"passed"`
	Observed    string   `json:"observed"`
	Limitations []string `json:"limitations,omitempty"`
}

type Provider interface {
	Name() string
	Exercise(Scenario) Result
}

type Harness struct{ providers []Provider }

func New(providers ...Provider) Harness {
	return Harness{providers: append([]Provider(nil), providers...)}
}

func (h Harness) Run() []Result {
	providers := append([]Provider(nil), h.providers...)
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].Name() < providers[j].Name() })
	results := make([]Result, 0, len(providers)*len(Scenarios))
	for _, provider := range providers {
		for _, scenario := range Scenarios {
			result := provider.Exercise(scenario)
			result.Provider = provider.Name()
			result.Scenario = scenario
			results = append(results, result)
		}
	}
	return results
}

// RecordedProvider is the adapter used by the comparison harness. The native
// implementation can populate it from live relay results; the CnCNet baseline
// populates limitations from measured behavior rather than claiming parity.
type RecordedProvider struct {
	ProviderName string
	Outcomes     map[Scenario]Result
}

func (p RecordedProvider) Name() string { return p.ProviderName }
func (p RecordedProvider) Exercise(scenario Scenario) Result {
	if result, ok := p.Outcomes[scenario]; ok {
		return result
	}
	return Result{Passed: false, Observed: "scenario was not recorded"}
}
