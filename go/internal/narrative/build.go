package narrative

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	powerNoiseW    = 200.0 // grid / EV / PV scene thresholds
	batteryActiveW = 100.0 // charge/discharge counts as intentional
	batteryQuietW  = 50.0  // below this we may say batteries are idle
	energyNoiseKWh = 0.3
	minEpisodeMin  = 20
	fuseHighPct    = 85.0
)

// Build extracts a FactBundle from Input. Pure — no I/O.
func Build(in Input) FactBundle {
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	if in.Window == "" {
		in.Window = WindowNow
	}

	b := FactBundle{
		Window: in.Window,
		Confidence: ConfidenceFact{
			Register:        "assertive",
			PriceConfidence: in.PriceConfidence,
			PVResidualHigh:  in.PVResidualHigh,
		},
	}
	if in.HaveConfidence && (in.PriceConfidence > 0 && in.PriceConfidence < 0.95 || in.PVResidualHigh) {
		b.Confidence.Register = "hedged"
	}

	if in.Window == WindowNow || in.Live.PVW != 0 || in.Live.LoadW != 0 || in.Live.EVW != 0 || in.Live.GridW != 0 {
		b.Now = liveNowFact(in.Live)
	}

	if in.Live.EVPluggedIn && in.Live.EVTargetSoCPct > 0 && !in.Live.EVDeadline.IsZero() {
		b.Intents = append(b.Intents, IntentFact{
			Kind:      "ev_deadline",
			TargetSoC: in.Live.EVTargetSoCPct,
			Deadline:  in.Live.EVDeadline,
			Label:     in.Live.EVName,
		})
	}

	b.Episodes = clusterEpisodes(in.PlanSlots, in.Now)
	b.Outlook = nextOutlook(in.PlanSlots, in.Now)
	b.Restraints = deriveRestraints(in, b)

	if in.LastReplanReason != "" && !in.LastReplanAt.IsZero() {
		// Only surface recent replans (last 2h) for the now strip.
		if in.Window != WindowNow || in.Now.Sub(in.LastReplanAt) < 2*time.Hour {
			b.Replans = append(b.Replans, ReplanEvent{
				At:     in.LastReplanAt,
				Reason: in.LastReplanReason,
			})
		}
	}

	if in.HaveMoney && (in.Window == WindowToday || in.Window == WindowDay) {
		rel := ""
		if in.WeekMedianSavedOre > 0 {
			ratio := in.SavedOre / in.WeekMedianSavedOre
			switch {
			case ratio >= 1.25:
				rel = "strong_week"
			case ratio <= 0.6:
				rel = "weak_week"
			default:
				rel = "typical"
			}
		}
		b.Money = &MoneyFacts{
			SavedOre:           in.SavedOre,
			BaselineOre:        in.BaselineOre,
			ActualOre:          in.ActualOre,
			WeekMedianSavedOre: in.WeekMedianSavedOre,
			Relative:           rel,
		}
	}

	return b
}

func liveNowFact(live LiveScene) *NowFact {
	importing := live.GridW > powerNoiseW
	evDom := live.EVW > powerNoiseW && live.EVW >= math.Abs(live.PVW)*0.5
	kind := KindIdle
	switch {
	case evDom && importing:
		kind = KindEVDominates
	// Prefer absorb/charge over "idle export/import" once any meaningful
	// battery power is visible. The old 200 W gate called ~180 W charge
	// "idle" while one pack was clearly taking solar.
	case live.BatW > batteryActiveW && !importing && math.Abs(live.PVW) > powerNoiseW:
		kind = KindAbsorbPV
	case live.BatW > batteryActiveW && importing:
		kind = KindChargeGrid
	case live.BatW < -batteryActiveW:
		kind = KindDischargeCover
	case importing:
		kind = KindIdleImport
	case live.GridW < -powerNoiseW:
		kind = KindIdleExport
	}
	targetLag := math.Abs(live.BatTargetW) >= batteryActiveW &&
		math.Abs(live.BatW) < batteryQuietW
	return &NowFact{
		Kind:         kind,
		PVW:          live.PVW,
		LoadW:        live.LoadW,
		GridW:        live.GridW,
		BatW:         live.BatW,
		BatTargetW:   live.BatTargetW,
		EVW:          live.EVW,
		BatSoC:       live.BatSoC,
		BatteryCount: live.BatteryCount,
		FusePct:      live.FusePct,
		Importing:    importing,
		EVDominates:  evDom,
		TargetLag:    targetLag,
	}
}

func clusterEpisodes(slots []PlanSlot, now time.Time) []Episode {
	if len(slots) == 0 {
		return nil
	}
	// Prefer past+current day slots for today stories; for outlook we
	// still cluster upcoming material kinds separately.
	var eps []Episode
	var cur *Episode
	for _, s := range slots {
		if s.Start.After(now) {
			continue // outlook handles future
		}
		k := KindFromReason(s.Reason)
		if k == KindIdle || k == KindIdleExport || k == KindIdleImport || k == KindUnknown {
			if cur != nil {
				eps = append(eps, *cur)
				cur = nil
			}
			continue
		}
		dtH := float64(s.LenMin) / 60.0
		if dtH <= 0 {
			dtH = 0.25
		}
		ekWh := math.Abs(s.BatteryW) * dtH / 1000.0
		end := s.Start.Add(time.Duration(s.LenMin) * time.Minute)
		if cur != nil && cur.Kind == k {
			cur.End = end
			cur.EnergyKWh += ekWh
			cur.MeanPriceOre = (cur.MeanPriceOre + s.PriceOre) / 2
			continue
		}
		if cur != nil {
			eps = append(eps, *cur)
		}
		cur = &Episode{
			Start:          s.Start,
			End:            end,
			Kind:           k,
			DominantReason: s.Reason,
			EnergyKWh:      ekWh,
			MeanPriceOre:   s.PriceOre,
		}
	}
	if cur != nil {
		eps = append(eps, *cur)
	}

	// Drop noise unless it's the only episode.
	filtered := make([]Episode, 0, len(eps))
	for _, e := range eps {
		durMin := e.End.Sub(e.Start).Minutes()
		if e.EnergyKWh < energyNoiseKWh && durMin < minEpisodeMin {
			continue
		}
		filtered = append(filtered, e)
	}
	if len(filtered) == 0 && len(eps) > 0 {
		filtered = eps[:1]
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].EnergyKWh > filtered[j].EnergyKWh
	})
	if len(filtered) > 2 {
		filtered = filtered[:2]
	}
	return filtered
}

func nextOutlook(slots []PlanSlot, now time.Time) *OutlookFact {
	var best *OutlookFact
	for _, s := range slots {
		if !s.Start.After(now) {
			continue
		}
		k := KindFromReason(s.Reason)
		if !outlookMaterial(k) {
			// Idle/import gaps must not end the scan — the evening
			// discharge often has a short hold between peak slices
			// (seen live: 20:00–20:30 discharge, 20:30–21:00 idle,
			// then discharge again through the night).
			continue
		}
		end := s.Start.Add(time.Duration(s.LenMin) * time.Minute)
		if best != nil && outlookFamily(best.Kind) == outlookFamily(k) &&
			!s.Start.After(best.End.Add(outlookBridge)) {
			best.End = end
			best.SoCPct = s.SoCPct
			best.PriceOre = s.PriceOre
			if k == KindDischargePeak {
				best.Kind = KindDischargePeak
				best.Reason = s.Reason
			}
			continue
		}
		if best != nil {
			break // different family or gap too large
		}
		best = &OutlookFact{
			Kind:     k,
			Start:    s.Start,
			End:      end,
			PriceOre: s.PriceOre,
			SoCPct:   s.SoCPct,
			Reason:   s.Reason,
		}
	}
	return best
}

// outlookBridge is how far ahead a later same-family slot may start
// after the current window end and still extend it. Covers brief idle
// holds inside an evening discharge without swallowing the next day's
// unrelated charge window.
const outlookBridge = 2 * time.Hour

func outlookMaterial(k Kind) bool {
	switch k {
	case KindDischargePeak, KindDischarge, KindDischargeCover,
		KindChargeGrid, KindAbsorbPV, KindCurtail:
		return true
	default:
		return false
	}
}

// outlookFamily groups kinds that should extend one outlook window.
func outlookFamily(k Kind) string {
	switch k {
	case KindDischargePeak, KindDischarge, KindDischargeCover:
		return "discharge"
	case KindChargeGrid, KindCharge, KindAbsorbPV:
		return "charge"
	case KindCurtail:
		return "curtail"
	default:
		return string(k)
	}
}

func deriveRestraints(in Input, b FactBundle) []RestraintFact {
	var out []RestraintFact
	live := in.Live

	fuseHigh := live.HaveFuse && live.FusePct >= fuseHighPct
	evDeadline := live.EVPluggedIn && !live.EVDeadline.IsZero() && live.EVDeadline.After(in.Now)
	evHeavy := live.EVW > powerNoiseW

	if fuseHigh && (evHeavy || live.BatW > powerNoiseW || live.GridW > powerNoiseW) {
		out = append(out, RestraintFact{
			WouldHave: "chase midday battery arbitrage",
			Instead:   "holding battery near idle",
			Because:   "fuse is near the limit",
		})
	}

	if evDeadline && evHeavy {
		label := "the car"
		if live.EVName != "" {
			label = live.EVName
		}
		out = append(out, RestraintFact{
			WouldHave: "export or store more surplus in the home battery",
			Instead:   "letting EV charging own the surplus",
			Because:   label + " has a charge deadline",
		})
	}

	if b.Now != nil && b.Now.EVDominates && b.Now.Importing && math.Abs(live.PVW) > powerNoiseW {
		// Specific live contradiction — import while sunny because of EV.
		out = append(out, RestraintFact{
			WouldHave: "stay off-grid on solar alone",
			Instead:   "importing a little from the grid",
			Because:   "EV load exceeds available solar after house consumption",
		})
	}

	if in.HaveConfidence && in.PVResidualHigh && live.BatW < powerNoiseW {
		out = append(out, RestraintFact{
			WouldHave: "charge harder from solar",
			Instead:   "staying cautious on battery charge",
			Because:   "PV forecast uncertainty is high",
		})
	}

	// Target lag: dispatch wants charge(+) or discharge(−) but measured
	// power is still idle. Site convention stays in Facts; prose uses
	// direction words + magnitude (never a leading minus in the sentence).
	if b.Now != nil && b.Now.TargetLag {
		switch {
		case live.BatTargetW > batteryActiveW:
			out = append(out, RestraintFact{
				WouldHave: "absorb surplus into the battery now",
				Instead:   "still exporting the surplus",
				Because:   "charge target not yet reflected in measured power",
			})
		case live.BatTargetW < -batteryActiveW:
			out = append(out, RestraintFact{
				WouldHave: "discharge to cover load or export",
				Instead:   "battery still idle",
				Because:   "discharge target not yet reflected in measured power",
			})
		}
	}

	// Cap at one restraint for the strip — prefer fuse, then EV deadline,
	// then target-lag, then twin caution.
	if len(out) > 1 {
		sort.SliceStable(out, func(i, j int) bool {
			return restraintPri(out[i].Because) < restraintPri(out[j].Because)
		})
		out = out[:1]
	}
	return out
}

func restraintPri(because string) int {
	switch {
	case strings.Contains(because, "fuse is near the limit"):
		return 0
	case strings.Contains(because, "charge deadline"):
		return 1
	case strings.Contains(because, "EV load exceeds"):
		return 2
	case strings.Contains(because, "charge target not yet reflected"),
		strings.Contains(because, "discharge target not yet reflected"):
		return 3
	case strings.Contains(because, "PV forecast uncertainty"):
		return 4
	default:
		return 99
	}
}
