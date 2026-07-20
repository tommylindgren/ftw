package narrative

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Compose turns a FactBundle into English prose. Every non-empty clause
// must be justified by a field in Facts (enforced by tests).
func Compose(b FactBundle) Narrative {
	n := Narrative{
		Register: b.Confidence.Register,
		Facts:    b,
	}
	if n.Register == "" {
		n.Register = "assertive"
	}

	hedged := n.Register == "hedged"

	if b.Window == WindowNow {
		n.Action = composeNowAction(b, hedged)
		n.Restraint = composeRestraint(b)
		n.Outlook = composeOutlook(b, hedged)
	} else {
		n.Headline = composeHeadline(b)
		n.Action = composeTodayAction(b, hedged)
		n.Restraint = composeRestraint(b)
		n.Outlook = composeOutlook(b, hedged)
	}

	if n.Action != "" {
		n.Body = append(n.Body, n.Action)
	}
	if n.Restraint != "" {
		n.Body = append(n.Body, n.Restraint)
	}
	if n.Outlook != "" {
		n.Body = append(n.Body, n.Outlook)
	}
	// Boring fallback: one calm line so the strip isn't empty.
	if len(n.Body) == 0 && b.Now != nil {
		n.Action = "Holding steady — nothing unusual right now."
		n.Body = []string{n.Action}
	}
	return n
}

func composeNowAction(b FactBundle, hedged bool) string {
	if b.Now == nil {
		return ""
	}
	n := b.Now
	evName := "the car"
	for _, in := range b.Intents {
		if in.Kind == "ev_deadline" && in.Label != "" {
			evName = in.Label
			break
		}
	}

	switch {
	case n.EVDominates && n.Importing && math.Abs(n.PVW) > powerNoiseW:
		return fmt.Sprintf(
			"Solar can’t cover house + car — I’m importing ~%s while %s takes %s. %s.",
			fmtPower(n.GridW), evName, fmtPower(n.EVW), batteryBarelyHelp(n.BatteryCount),
		)
	case n.EVDominates && math.Abs(n.PVW) > powerNoiseW:
		return fmt.Sprintf(
			"Almost all solar is feeding %s (~%s); the house takes the rest.",
			evName, fmtPower(n.EVW),
		)
	case n.Kind == KindAbsorbPV:
		verb := "I’m absorbing"
		if hedged {
			verb = "I’m cautiously absorbing"
		}
		noun := batteryNoun(n.BatteryCount)
		if n.GridW < -powerNoiseW {
			return fmt.Sprintf("%s solar surplus into the %s (~%s) and exporting the rest (~%s).",
				verb, noun, fmtPower(n.BatW), fmtPower(math.Abs(n.GridW)))
		}
		return fmt.Sprintf("%s solar surplus into the %s (~%s).", verb, noun, fmtPower(n.BatW))
	case n.Kind == KindChargeGrid:
		return fmt.Sprintf("Charging the %s from the grid (~%s).", batteryNoun(n.BatteryCount), fmtPower(n.BatW))
	case n.Kind == KindDischargeCover || n.Kind == KindDischargePeak || n.Kind == KindDischarge:
		verb := "Discharging"
		if hedged {
			verb = "Likely discharging"
		}
		return fmt.Sprintf("%s the %s (~%s) to cover the house.", verb, batteryNoun(n.BatteryCount), fmtPower(math.Abs(n.BatW)))
	case n.Kind == KindIdleExport:
		return fmt.Sprintf("Exporting surplus solar (~%s); %s.",
			fmtPower(math.Abs(n.GridW)), batterySideClause(n.BatW, n.BatteryCount))
	case n.Kind == KindIdleImport:
		return fmt.Sprintf("Importing ~%s to cover the house; %s.",
			fmtPower(n.GridW), batterySideClause(n.BatW, n.BatteryCount))
	default:
		if math.Abs(n.BatW) < powerNoiseW && math.Abs(n.EVW) < powerNoiseW {
			return ""
		}
		return fmt.Sprintf("Live mix: PV generating ~%s, load ~%s, %s.",
			fmtPower(n.PVW), fmtPower(n.LoadW), gridPowerPhrase(n.GridW))
	}
}

func composeTodayAction(b FactBundle, hedged bool) string {
	if len(b.Episodes) == 0 {
		if b.Now != nil {
			return composeNowAction(b, hedged)
		}
		return ""
	}
	parts := make([]string, 0, 2)
	for _, e := range b.Episodes {
		parts = append(parts, episodeClause(e))
	}
	prefix := "I"
	if hedged {
		prefix = "I likely"
	}
	if len(parts) == 1 {
		return prefix + " " + parts[0] + "."
	}
	return prefix + " " + parts[0] + ", then " + parts[1] + "."
}

func episodeClause(e Episode) string {
	win := formatWindow(e.Start, e.End)
	switch e.Kind {
	case KindAbsorbPV:
		return fmt.Sprintf("took solar surplus %s", win)
	case KindChargeGrid:
		return fmt.Sprintf("charged from the grid %s", win)
	case KindDischargePeak:
		return fmt.Sprintf("covered the price peak %s", win)
	case KindDischargeCover, KindDischarge:
		return fmt.Sprintf("discharged to cover load %s", win)
	case KindCurtail:
		return fmt.Sprintf("curtailed PV %s", win)
	default:
		return fmt.Sprintf("ran %s %s", e.Kind, win)
	}
}

func composeRestraint(b FactBundle) string {
	if len(b.Restraints) == 0 {
		return ""
	}
	r := b.Restraints[0]
	// Prefer the plan's canned phrasing for the Live fixture.
	if strings.Contains(r.Because, "fuse is near the limit") {
		extra := ""
		for _, in := range b.Intents {
			if in.Kind == "ev_deadline" {
				extra = " and the car has a charge deadline"
				break
			}
		}
		return "Holding back: not chasing midday arbitrage; fuse is near the limit" + extra + "."
	}
	if strings.Contains(r.Because, "charge deadline") {
		return "Holding back: letting EV charging own the surplus ahead of the charge deadline."
	}
	if strings.Contains(r.Because, "EV load exceeds") {
		return "Holding back: solar alone can’t cover house + car, so a little grid import remains."
	}
	if strings.Contains(r.Because, "PV forecast uncertainty") {
		return "Holding back: staying cautious on battery charge while PV forecast uncertainty is high."
	}
	if strings.Contains(r.Because, "charge target not yet reflected") ||
		strings.Contains(r.Because, "discharge target not yet reflected") {
		tw := 0.0
		noun := "battery"
		if b.Now != nil {
			tw = b.Now.BatTargetW
			noun = batteryNoun(b.Now.BatteryCount)
		}
		return fmt.Sprintf("Holding back: %s should be %s but measured power is still idle — surplus stays on the grid for now.",
			noun, batteryPowerPhrase(tw))
	}
	return fmt.Sprintf("Holding back: %s instead of %s — %s.", r.Instead, r.WouldHave, r.Because)
}

func composeOutlook(b FactBundle, hedged bool) string {
	if b.Outlook == nil {
		return ""
	}
	o := b.Outlook
	win := formatWindow(o.Start, o.End)
	verb := "expect"
	if hedged {
		verb = "may see"
	}
	switch o.Kind {
	case KindDischargePeak, KindDischarge, KindDischargeCover:
		soc := ""
		if o.SoCPct > 0 {
			soc = fmt.Sprintf(" (SoC ~%.0f%%)", o.SoCPct)
		}
		// This is the plan's discharge window, not the full spot-price
		// peak from the chart — keep the wording honest.
		return fmt.Sprintf("Next: %s discharge window %s — %s discharge%s.",
			batteryNoun(batteryCountFrom(b)), win, verb, soc)
	case KindChargeGrid:
		return fmt.Sprintf("Next: cheap-grid charging window %s.", win)
	case KindAbsorbPV:
		return fmt.Sprintf("Next: %s absorb window %s.", batteryNoun(batteryCountFrom(b)), win)
	case KindCurtail:
		return fmt.Sprintf("Next: possible PV curtailment %s.", win)
	default:
		return fmt.Sprintf("Next: %s %s.", o.Kind, win)
	}
}

func batteryCountFrom(b FactBundle) int {
	if b.Now != nil {
		return b.Now.BatteryCount
	}
	return 0
}

func composeHeadline(b FactBundle) string {
	if b.Money == nil {
		return ""
	}
	sek := b.Money.SavedOre / 100.0
	sign := "+"
	if sek < 0 {
		sign = ""
	}
	rel := ""
	switch b.Money.Relative {
	case "strong_week":
		rel = " — strong vs your recent days"
	case "weak_week":
		rel = " — weaker than your recent days"
	case "typical":
		rel = " — about typical for you"
	}
	return fmt.Sprintf("%s%.0f SEK saved vs no PV/battery%s", sign, sek, rel)
}

func fmtPower(w float64) string {
	// Magnitude only — site-convention sign is expressed with words
	// (charging/discharging, importing/exporting), never a leading "-".
	// See docs/site-convention.md: UI displays direction in language;
	// Facts keep signed W.
	aw := math.Abs(w)
	if aw >= 1000 {
		return fmt.Sprintf("%.1f kW", aw/1000)
	}
	return fmt.Sprintf("%.0f W", aw)
}

// batteryPowerPhrase maps a site-convention battery power to prose:
// +W → "charging ~X", −W → "discharging ~X".
func batteryPowerPhrase(siteW float64) string {
	switch {
	case siteW > batteryQuietW:
		return "charging ~" + fmtPower(siteW)
	case siteW < -batteryQuietW:
		return "discharging ~" + fmtPower(siteW)
	default:
		return "idle"
	}
}

// gridPowerPhrase maps site-convention grid power:
// +W → "importing ~X", −W → "exporting ~X".
func gridPowerPhrase(siteW float64) string {
	switch {
	case siteW > powerNoiseW:
		return "importing ~" + fmtPower(siteW)
	case siteW < -powerNoiseW:
		return "exporting ~" + fmtPower(siteW)
	default:
		return "near zero on the grid"
	}
}

// batteryNoun returns "battery" or "batteries". Count <= 1 → singular
// (including unknown/0, so we don't invent a fleet).
func batteryNoun(count int) string {
	if count >= 2 {
		return "batteries"
	}
	return "battery"
}

func batteryBarelyHelp(count int) string {
	if count >= 2 {
		return "Batteries barely help"
	}
	return "Battery barely helps"
}

// batterySideClause describes aggregate battery behaviour without
// claiming "idle" when a pack is clearly taking or giving power.
func batterySideClause(batW float64, count int) string {
	noun := batteryNoun(count)
	switch {
	case math.Abs(batW) < batteryQuietW:
		return noun + " idle"
	case batW >= batteryQuietW && batW < batteryActiveW:
		return fmt.Sprintf("%s absorbing a little (~%s)", noun, fmtPower(batW))
	case batW >= batteryActiveW:
		return fmt.Sprintf("%s absorbing (~%s)", noun, fmtPower(batW))
	case batW <= -batteryQuietW && batW > -batteryActiveW:
		return fmt.Sprintf("%s contributing a little (~%s)", noun, fmtPower(math.Abs(batW)))
	default:
		return fmt.Sprintf("%s contributing (~%s)", noun, fmtPower(math.Abs(batW)))
	}
}

func formatWindow(start, end time.Time) string {
	if start.IsZero() {
		return ""
	}
	if end.IsZero() || !end.After(start) {
		return fmt.Sprintf("~%02d:%02d", start.Hour(), start.Minute())
	}
	// Always include minutes so a 15-minute slot at 16:00 does not
	// collapse to the misleading "16–16".
	return fmt.Sprintf("%02d:%02d–%02d:%02d", start.Hour(), start.Minute(), end.Hour(), end.Minute())
}
