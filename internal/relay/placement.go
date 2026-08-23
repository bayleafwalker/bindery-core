package relay

import (
	"sort"
	"strings"
)

const PlacementPolicyVersion = "latency-capacity-v1"

type PlacementRequest struct {
	AllowedRegions []string
	ParticipantIDs []string
	LatencyP95MS   int
	PolicyVersion  string
}

type Candidate struct {
	ProviderID             string
	RelayID                string
	Region                 string
	State                  State
	RemainingPacketRate    int
	RemainingEgressBytesPS int
	ParticipantRTTMS       map[string]int
}

type CandidateInput struct {
	ProviderID             string         `json:"provider_id"`
	RelayID                string         `json:"relay_id"`
	Region                 string         `json:"region"`
	State                  State          `json:"state"`
	RemainingPacketRate    int            `json:"remaining_packet_rate"`
	RemainingEgressBytesPS int            `json:"remaining_egress_bytes_per_second"`
	ParticipantRTTMS       map[string]int `json:"participant_rtt_ms"`
}

type Decision struct {
	PolicyVersion           string           `json:"policy_version"`
	ProviderID              string           `json:"provider_id"`
	RelayID                 string           `json:"relay_id"`
	Region                  string           `json:"region"`
	MaximumParticipantRTTMS int              `json:"maximum_participant_rtt_ms"`
	MeanParticipantRTTMS    float64          `json:"mean_participant_rtt_ms"`
	P95ParticipantRTTMS     int              `json:"p95_participant_rtt_ms"`
	RemainingPacketRate     int              `json:"remaining_packet_rate"`
	RemainingEgressBytesPS  int              `json:"remaining_egress_bytes_per_second"`
	Inputs                  []CandidateInput `json:"inputs"`
}

func ChoosePlacement(request PlacementRequest, candidates []Candidate) (Decision, error) {
	policy := request.PolicyVersion
	if policy == "" {
		policy = PlacementPolicyVersion
	}
	allowed := make(map[string]struct{}, len(request.AllowedRegions))
	for _, region := range request.AllowedRegions {
		allowed[region] = struct{}{}
	}
	if len(allowed) == 0 || len(request.ParticipantIDs) == 0 {
		return Decision{}, ErrNoRelayCapacity
	}
	inputs := make([]CandidateInput, 0, len(candidates))
	type ranked struct {
		candidate Candidate
		maximum   int
		mean      float64
		p95       int
	}
	eligible := make([]ranked, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.State != Accepting {
			continue
		}
		if _, ok := allowed[candidate.Region]; !ok {
			continue
		}
		values := make([]int, 0, len(request.ParticipantIDs))
		valid := true
		for _, participant := range request.ParticipantIDs {
			rtt, ok := candidate.ParticipantRTTMS[participant]
			if !ok || rtt < 0 {
				valid = false
				break
			}
			values = append(values, rtt)
		}
		if !valid {
			continue
		}
		sort.Ints(values)
		maximum := values[len(values)-1]
		if request.LatencyP95MS > 0 && maximum > request.LatencyP95MS {
			continue
		}
		sum := 0
		for _, value := range values {
			sum += value
		}
		p95Index := (len(values)*95+99)/100 - 1
		if p95Index < 0 {
			p95Index = 0
		}
		eligible = append(eligible, ranked{candidate: candidate, maximum: maximum, mean: float64(sum) / float64(len(values)), p95: values[p95Index]})
	}
	for _, candidate := range candidates {
		input := CandidateInput{ProviderID: candidate.ProviderID, RelayID: candidate.RelayID, Region: candidate.Region, State: candidate.State, RemainingPacketRate: candidate.RemainingPacketRate, RemainingEgressBytesPS: candidate.RemainingEgressBytesPS, ParticipantRTTMS: cloneRTTs(candidate.ParticipantRTTMS)}
		inputs = append(inputs, input)
	}
	if len(eligible) == 0 {
		return Decision{PolicyVersion: policy, Inputs: inputs}, ErrNoRelayCapacity
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.maximum != b.maximum {
			return a.maximum < b.maximum
		}
		if a.mean != b.mean {
			return a.mean < b.mean
		}
		if a.p95 != b.p95 {
			return a.p95 < b.p95
		}
		if a.candidate.RemainingPacketRate != b.candidate.RemainingPacketRate {
			return a.candidate.RemainingPacketRate > b.candidate.RemainingPacketRate
		}
		if a.candidate.RemainingEgressBytesPS != b.candidate.RemainingEgressBytesPS {
			return a.candidate.RemainingEgressBytesPS > b.candidate.RemainingEgressBytesPS
		}
		provider := strings.Compare(a.candidate.ProviderID, b.candidate.ProviderID)
		if provider != 0 {
			return provider < 0
		}
		return a.candidate.RelayID < b.candidate.RelayID
	})
	chosen := eligible[0]
	return Decision{PolicyVersion: policy, ProviderID: chosen.candidate.ProviderID, RelayID: chosen.candidate.RelayID, Region: chosen.candidate.Region, MaximumParticipantRTTMS: chosen.maximum, MeanParticipantRTTMS: chosen.mean, P95ParticipantRTTMS: chosen.p95, RemainingPacketRate: chosen.candidate.RemainingPacketRate, RemainingEgressBytesPS: chosen.candidate.RemainingEgressBytesPS, Inputs: inputs}, nil
}

func cloneRTTs(values map[string]int) map[string]int {
	result := make(map[string]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
