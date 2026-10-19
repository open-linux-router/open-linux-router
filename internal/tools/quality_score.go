package tools

import (
	"math"
	"slices"
	"time"
)

type QualityComponent struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Weight          float64  `json:"weight"`
	EffectiveWeight float64  `json:"effective_weight"`
	Score           *float64 `json:"score"`
	Reason          string   `json:"reason,omitempty"`
}

type QualityCategory struct {
	ID         string             `json:"id"`
	Label      string             `json:"label"`
	Budget     float64            `json:"budget"`
	State      string             `json:"state"`
	Score      *float64           `json:"score"`
	Earned     float64            `json:"earned_points"`
	Available  float64            `json:"available_points"`
	Components []QualityComponent `json:"components"`
}

type QualityOverall struct {
	State     string   `json:"state"`
	Score     *float64 `json:"score"`
	Earned    float64  `json:"earned_points"`
	Available float64  `json:"available_points"`
	Coverage  float64  `json:"coverage_percent"`
}

type scoreAnchor struct{ value, score float64 }

var downloadCurve = []scoreAnchor{{0, 0}, {3, 20}, {10, 40}, {25, 60}, {75, 80}, {150, 90}, {500, 100}}
var uploadCurve = []scoreAnchor{{0, 0}, {1, 20}, {3, 40}, {10, 60}, {30, 80}, {75, 90}, {200, 100}}
var tcpCurve = []scoreAnchor{{0, 100}, {10, 100}, {30, 90}, {60, 75}, {100, 60}, {200, 35}, {500, 0}}
var dnsCurve = []scoreAnchor{{0, 100}, {10, 100}, {30, 90}, {60, 75}, {100, 60}, {200, 35}, {500, 0}}
var responseCurve = []scoreAnchor{{0, 100}, {30, 100}, {100, 90}, {200, 75}, {500, 50}, {1000, 20}, {2000, 0}}
var variationCurve = []scoreAnchor{{0, 100}, {2, 95}, {5, 85}, {10, 70}, {20, 50}, {50, 20}, {100, 0}}

func scoreValue(v float64, curve []scoreAnchor) *float64 {
	if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	if v <= curve[0].value {
		return valuePointer(curve[0].score)
	}
	for i := 1; i < len(curve); i++ {
		a, b := curve[i-1], curve[i]
		if v <= b.value {
			return valuePointer(a.score + (b.score-a.score)*(v-a.value)/(b.value-a.value))
		}
	}
	return valuePointer(curve[len(curve)-1].score)
}

func valuePointer(v float64) *float64 { return &v }

func medianScore(samples []QualitySample, field func(QualitySample) *float64, curve []scoreAnchor) *float64 {
	var values []float64
	for _, sample := range samples {
		if v := field(sample); v != nil && *v >= 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
			values = append(values, *v)
		}
	}
	if len(values) < 4 {
		return nil
	}
	slices.Sort(values)
	middle := len(values) / 2
	median := values[middle]
	if len(values)%2 == 0 {
		median = (values[middle-1] + values[middle]) / 2
	}
	return scoreValue(median, curve)
}

func timingScores(samples []QualitySample) [3]*float64 {
	return [3]*float64{
		medianScore(samples, func(s QualitySample) *float64 { return s.TCP }, tcpCurve),
		medianScore(samples, func(s QualitySample) *float64 { return s.DNS }, dnsCurve),
		medianScore(samples, func(s QualitySample) *float64 { return s.Response }, responseCurve),
	}
}

func stabilityScores(samples []QualitySample) [2]*float64 {
	var out [2]*float64
	if len(samples) < qualitySamples {
		return out
	}
	connected, attempted := 0, 0
	var delta float64
	pairs := 0
	var previous *float64
	for _, s := range samples {
		if s.State == "error" || s.State == "cancelled" {
			previous = nil
			continue
		}
		attempted++
		if s.TCP != nil {
			connected++
			if previous != nil {
				delta += math.Abs(*s.TCP - *previous)
				pairs++
			}
		}
		previous = s.TCP
	}
	if attempted == qualitySamples {
		out[0] = valuePointer(100 * float64(connected) / float64(attempted))
	}
	if pairs >= 3 {
		out[1] = scoreValue(delta/float64(pairs), variationCurve)
	}
	return out
}

// Alternatives choose one whole family result, not independently selected
// timing fields from incompatible paths. Compare normalized measured scores.
func bestFamily(results []QualityTargetResult, id string, stability bool) []QualitySample {
	var best []QualitySample
	bestScore := -1.0
	bestWeight := -1.0
	for _, r := range results {
		if r.ID != id {
			continue
		}
		var scores []*float64
		var weights []float64
		if stability {
			v := stabilityScores(r.Samples)
			scores = v[:]
			weights = []float64{.4, .3}
		} else {
			v := timingScores(r.Samples)
			scores = v[:]
			weights = []float64{.6, .15, .25}
		}
		total := 0.0
		weight := 0.0
		for i, s := range scores {
			if s != nil {
				total += *s * weights[i]
				weight += weights[i]
			}
		}
		if weight > 0 {
			total /= weight
		} else {
			total = -1
		}
		if total > bestScore || (total == bestScore && weight > bestWeight) {
			bestScore = total
			bestWeight = weight
			best = r.Samples
		}
	}
	return best
}

func accessScore(results []QualityTargetResult, id string) *float64 {
	knownFailure := false
	unknown := false
	families := make(map[string]bool)
	for _, r := range results {
		if r.ID != id {
			continue
		}
		families[r.Family] = true
		if len(r.Samples) != qualitySamples {
			unknown = true
		}
		for _, s := range r.Samples {
			if s.State == "ok" {
				return valuePointer(100)
			}
			if s.State == "failed" {
				knownFailure = true
			} else {
				unknown = true
			}
		}
	}
	if knownFailure && !unknown && families["ipv4"] && families["ipv6"] {
		return valuePointer(0)
	}
	return nil
}

func category(id, label string, budget float64, components ...QualityComponent) QualityCategory {
	c := QualityCategory{ID: id, Label: label, Budget: budget, State: "unavailable", Components: components}
	weighted, weight := 0.0, 0.0
	for _, item := range components {
		if item.Score == nil {
			continue
		}
		weighted += *item.Score * item.Weight
		weight += item.Weight
	}
	if weight > 0 {
		c.Score = valuePointer(weighted / weight)
		for i := range c.Components {
			if c.Components[i].Score != nil {
				c.Components[i].EffectiveWeight = c.Components[i].Weight / weight
			}
		}
		c.State = "partial"
		if weight >= 1-1e-9 {
			c.State = "ready"
		}
	}
	return c
}

func scoreQuality(s QualitySnapshot, now time.Time) ([]QualityCategory, QualityOverall) {
	reference := QualityComponent{ID: "reference", Label: "Reference download and upload", Weight: .4, Reason: "Bandwidth test not run"}
	if s.Speed != nil {
		d, u := scoreValue(s.Speed.Download, downloadCurve), scoreValue(s.Speed.Upload, uploadCurve)
		if d != nil && u != nil {
			reference.Score = valuePointer(*d*.7 + *u*.3)
			reference.Reason = "Single reference server; service transfers not measured"
		}
	}
	if s.SpeedError != "" {
		reference.Reason = s.SpeedError
	}
	categories := []QualityCategory{category("speed", "Speed", 25, reference, QualityComponent{ID: "services", Label: "Service-specific transfers", Weight: .6, Reason: "Not implemented"})}
	latency, stability, functions := []QualityComponent{}, []QualityComponent{}, []QualityComponent{}
	for _, target := range qualityTargets {
		timing := timingScores(bestFamily(s.Targets, target.ID, false))
		for i, item := range []struct {
			id, label string
			weight    float64
		}{{"tcp", "TCP connection", .6}, {"dns", "DNS lookup", .15}, {"response", "HTTPS response", .25}} {
			latency = append(latency, QualityComponent{ID: target.ID + "-" + item.id, Label: target.Name + " · " + item.label, Weight: item.weight / float64(len(qualityTargets)), Score: timing[i], Reason: "Median of at least four completed timings; best measured IP family"})
		}
		consistency := stabilityScores(bestFamily(s.Targets, target.ID, true))
		stability = append(stability,
			QualityComponent{ID: target.ID + "-continuity", Label: target.Name + " · TCP connection success", Weight: .4 / float64(len(qualityTargets)), Score: consistency[0], Reason: "Six connection attempts; not packet-loss measurement"},
			QualityComponent{ID: target.ID + "-variation", Label: target.Name + " · TCP timing variation", Weight: .3 / float64(len(qualityTargets)), Score: consistency[1], Reason: "Mean absolute difference between consecutive completed TCP timings"})
		functions = append(functions, QualityComponent{ID: target.ID + "-access", Label: target.Name + " · Test resource access", Weight: .5 / float64(len(qualityTargets)), Score: accessScore(s.Targets, target.ID), Reason: "IPv4 or IPv6; expected resource response, not full service functionality"})
	}
	stability = append(stability, QualityComponent{ID: "load", Label: "Consistency under load", Weight: .3, Reason: "Not implemented"})
	functions = append(functions, QualityComponent{ID: "protocols", Label: "Protocol capabilities", Weight: .25, Reason: "Not implemented"}, QualityComponent{ID: "inbound-p2p", Label: "Inbound and P2P", Weight: .25, Reason: "Not implemented; STUN observations alone do not verify this capability"})
	categories = append(categories, category("latency", "Latency", 25, latency...), category("stability", "Stability", 25, stability...), category("wifi", "Wi-Fi", 15, QualityComponent{ID: "client-link", Label: "Measured wireless links", Weight: 1, Reason: "Requires client or AP measurements"}), category("functionality", "Functionality", 10, functions...))
	return aggregateQuality(categories, s, now)
}

func aggregateQuality(categories []QualityCategory, s QualitySnapshot, now time.Time) ([]QualityCategory, QualityOverall) {
	o := QualityOverall{State: "unavailable"}
	budget := 0.0
	for i := range categories {
		c := &categories[i]
		c.Earned, c.Available = 0, 0
		if c.Score == nil {
			continue
		}
		budget += c.Budget
		for _, component := range c.Components {
			if component.Score != nil {
				o.Coverage += c.Budget * component.Weight
			}
		}
	}
	if budget > 0 {
		for i := range categories {
			c := &categories[i]
			if c.Score == nil {
				continue
			}
			c.Available = 100 * c.Budget / budget
			c.Earned = *c.Score * c.Available / 100
			o.Earned += c.Earned
		}
		o.Available = 100
		o.State = "ready"
		o.Score = valuePointer(o.Earned)
	}
	if s.State == "running" {
		o.State = "measuring"
		o.Score = nil
	}
	if s.State == "cancelled" || s.State == "timed_out" {
		o.State = "partial"
		o.Score = nil
	}
	return categories, o
}
