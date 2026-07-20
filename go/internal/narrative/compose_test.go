package narrative

import (
	"strings"
	"testing"
	"time"
)

func TestKindFromReason(t *testing.T) {
	cases := map[string]Kind{
		"absorb PV surplus":                   KindAbsorbPV,
		"charge from cheap grid":              KindChargeGrid,
		"charge — import":                     KindChargeGrid,
		"discharge — export at peak":          KindDischargePeak,
		"discharge — cover local load":        KindDischargeCover,
		"idle — import to cover load":         KindIdleImport,
		"idle — export PV surplus":            KindIdleExport,
		"curtail PV (negative export) · idle": KindCurtail,
		"":                                    KindUnknown,
	}
	for in, want := range cases {
		if got := KindFromReason(in); got != want {
			t.Errorf("KindFromReason(%q)=%q want %q", in, got, want)
		}
	}
}

func TestComposeNow_EVEatsPVWithImport(t *testing.T) {
	deadline := time.Date(2026, 7, 21, 11, 0, 0, 0, time.Local)
	now := time.Date(2026, 7, 20, 12, 10, 0, 0, time.Local)
	in := Input{
		Window: WindowNow,
		Now:    now,
		Live: LiveScene{
			PVW:            -15220,
			LoadW:          5420,
			GridW:          3210,
			BatW:           50,
			EVW:            11100,
			BatSoC:         60,
			FusePct:        92,
			HaveFuse:       true,
			EVPluggedIn:    true,
			EVTargetSoCPct: 80,
			EVDeadline:     deadline,
			EVName:         "Easee",
		},
		PlanSlots: []PlanSlot{
			{
				Start:    now.Add(7 * time.Hour),
				LenMin:   60,
				Reason:   "discharge — export at peak",
				BatteryW: -3000,
				PriceOre: 140,
				SoCPct:   45,
			},
		},
	}
	n := Compose(Build(in))

	if n.Register != "assertive" {
		t.Errorf("register=%q", n.Register)
	}
	if !strings.Contains(n.Action, "importing") {
		t.Fatalf("action missing import story: %q", n.Action)
	}
	if !strings.Contains(n.Action, "Easee") {
		t.Fatalf("action should name charger: %q", n.Action)
	}
	if n.Restraint == "" || !strings.Contains(n.Restraint, "fuse") {
		t.Fatalf("restraint should mention fuse: %q", n.Restraint)
	}
	if n.Outlook == "" || !strings.Contains(strings.ToLower(n.Outlook), "discharge") {
		t.Fatalf("outlook should mention discharge: %q", n.Outlook)
	}
	assertTraceable(t, n)
}

func TestComposeNow_ExportWithPartialBatteryCharge(t *testing.T) {
	// Regression: site export with aggregate ~169 W charge (one pack
	// idle, one at ~180 W) must not say "battery is idle".
	now := time.Date(2026, 7, 20, 13, 0, 0, 0, time.Local)
	in := Input{
		Window: WindowNow,
		Now:    now,
		Live: LiveScene{
			PVW:          -16000,
			LoadW:        2170,
			GridW:        -13660,
			BatW:         169, // pixii-1 ≈0 + pixii-2 ≈181
			EVW:          0,
			BatteryCount: 2,
		},
	}
	n := Compose(Build(in))
	if strings.Contains(strings.ToLower(n.Action), "idle") {
		t.Fatalf("must not claim idle while batteries charge: %q", n.Action)
	}
	if !strings.Contains(n.Action, "batteries") {
		t.Fatalf("expected plural batteries: %q", n.Action)
	}
	assertTraceable(t, n)
}

func TestComposeNow_SingleBatterySingular(t *testing.T) {
	now := time.Date(2026, 7, 20, 13, 0, 0, 0, time.Local)
	in := Input{
		Window: WindowNow,
		Now:    now,
		Live: LiveScene{
			PVW:          -5000,
			LoadW:        2000,
			GridW:        -2000,
			BatW:         1000,
			BatteryCount: 1,
		},
	}
	n := Compose(Build(in))
	if strings.Contains(n.Action, "batteries") {
		t.Fatalf("single pack must use singular: %q", n.Action)
	}
	if !strings.Contains(n.Action, "battery") {
		t.Fatalf("expected singular battery: %q", n.Action)
	}
	assertTraceable(t, n)
}

func TestComposeNow_BigExportTargetLag(t *testing.T) {
	// Scene from Live: ~14 kW export, battery measured idle, dispatch
	// target ~500 W charge — tell the gap with direction words, never
	// a signed watt string, and never render "16–16".
	now := time.Date(2026, 7, 20, 15, 30, 0, 0, time.Local)
	in := Input{
		Window: WindowNow,
		Now:    now,
		Live: LiveScene{
			PVW:          -14530,
			LoadW:        337,
			GridW:        -14190,
			BatW:         0,
			BatTargetW:   500,
			BatteryCount: 1,
		},
		PlanSlots: []PlanSlot{
			{
				Start:    time.Date(2026, 7, 20, 16, 0, 0, 0, time.Local),
				LenMin:   15,
				Reason:   "absorb PV surplus",
				BatteryW: 2000,
			},
		},
	}
	n := Compose(Build(in))
	if !strings.Contains(strings.ToLower(n.Action), "exporting") {
		t.Fatalf("action=%q", n.Action)
	}
	if !strings.Contains(strings.ToLower(n.Action), "idle") {
		t.Fatalf("action should note idle battery: %q", n.Action)
	}
	if n.Restraint == "" || !strings.Contains(n.Restraint, "charging ~500 W") {
		t.Fatalf("restraint should say charging ~500 W: %q", n.Restraint)
	}
	if strings.Contains(n.Outlook, "16–16") || strings.Contains(n.Outlook, "16-16") {
		t.Fatalf("outlook must not collapse to 16–16: %q", n.Outlook)
	}
	if !strings.Contains(n.Outlook, "16:00") {
		t.Fatalf("outlook should show start time: %q", n.Outlook)
	}
	assertTraceable(t, n)
}

func TestComposeNow_DischargeTargetLag(t *testing.T) {
	// Dispatch wants discharge (−979 W site convention) but measured
	// bat is still idle — prose must say "discharging ~979 W", never "-979".
	now := time.Date(2026, 7, 20, 20, 5, 0, 0, time.Local)
	in := Input{
		Window: WindowNow,
		Now:    now,
		Live: LiveScene{
			PVW:          0,
			LoadW:        1200,
			GridW:        1200,
			BatW:         0,
			BatTargetW:   -979,
			BatteryCount: 1,
		},
	}
	n := Compose(Build(in))
	if n.Restraint == "" || !strings.Contains(n.Restraint, "discharging ~979 W") {
		t.Fatalf("restraint=%q", n.Restraint)
	}
	if strings.Contains(n.Restraint, "-979") {
		t.Fatalf("must not leak signed watts: %q", n.Restraint)
	}
	assertTraceable(t, n)
}

func TestNextOutlook_BridgesIdleGapInDischargeEvening(t *testing.T) {
	// Live plan shape: 20:00–20:30 discharge, 20:30–21:00 idle, then
	// discharge again through the evening — must not stop at 20:30.
	now := time.Date(2026, 7, 20, 16, 0, 0, 0, time.Local)
	slots := []PlanSlot{
		{Start: time.Date(2026, 7, 20, 20, 0, 0, 0, time.Local), LenMin: 15, Reason: "discharge — price above horizon mean", BatteryW: -700},
		{Start: time.Date(2026, 7, 20, 20, 15, 0, 0, time.Local), LenMin: 15, Reason: "discharge — price above horizon mean", BatteryW: -900},
		{Start: time.Date(2026, 7, 20, 20, 30, 0, 0, time.Local), LenMin: 15, Reason: "idle — import to cover load", BatteryW: 0},
		{Start: time.Date(2026, 7, 20, 20, 45, 0, 0, time.Local), LenMin: 15, Reason: "idle — import to cover load", BatteryW: 0},
		{Start: time.Date(2026, 7, 20, 21, 0, 0, 0, time.Local), LenMin: 15, Reason: "discharge — price above horizon mean", BatteryW: -800},
		{Start: time.Date(2026, 7, 20, 21, 15, 0, 0, time.Local), LenMin: 15, Reason: "discharge — price above horizon mean", BatteryW: -800},
		{Start: time.Date(2026, 7, 20, 22, 0, 0, 0, time.Local), LenMin: 15, Reason: "discharge — price above horizon mean", BatteryW: -1600},
		{Start: time.Date(2026, 7, 21, 0, 45, 0, 0, time.Local), LenMin: 15, Reason: "discharge — price above horizon mean", BatteryW: -500},
		{Start: time.Date(2026, 7, 21, 1, 0, 0, 0, time.Local), LenMin: 15, Reason: "discharge — cover local load", BatteryW: -500},
	}
	o := nextOutlook(slots, now)
	if o == nil {
		t.Fatal("expected outlook")
	}
	wantStart := time.Date(2026, 7, 20, 20, 0, 0, 0, time.Local)
	if !o.Start.Equal(wantStart) {
		t.Fatalf("start=%v", o.Start)
	}
	// Must bridge the 20:30–21:00 idle and keep extending; a regression
	// stops at 20:30.
	if !o.End.After(time.Date(2026, 7, 20, 21, 0, 0, 0, time.Local)) {
		t.Fatalf("failed to bridge idle gap: window %s", formatWindow(o.Start, o.End))
	}
}

func TestNextOutlook_MergesDischargeFamily(t *testing.T) {
	now := time.Date(2026, 7, 20, 16, 0, 0, 0, time.Local)
	slots := []PlanSlot{
		{Start: now.Add(4 * time.Hour), LenMin: 30, Reason: "discharge — export at peak", BatteryW: -3000, SoCPct: 88},
		{Start: now.Add(4*time.Hour + 30*time.Minute), LenMin: 60, Reason: "discharge — cover local load", BatteryW: -2000, SoCPct: 80},
		{Start: now.Add(5*time.Hour + 30*time.Minute), LenMin: 60, Reason: "discharge — price above horizon mean", BatteryW: -1500, SoCPct: 70},
	}
	o := nextOutlook(slots, now)
	if o == nil {
		t.Fatal("expected outlook")
	}
	// 20:00 + 30m + 60m + 60m = 22:30
	wantEnd := now.Add(4*time.Hour + 30*time.Minute + 60*time.Minute + 60*time.Minute)
	if !o.Start.Equal(now.Add(4 * time.Hour)) {
		t.Fatalf("start=%v", o.Start)
	}
	if !o.End.Equal(wantEnd) {
		t.Fatalf("end=%v want %v (window %s)", o.End, wantEnd, formatWindow(o.Start, o.End))
	}
	n := Compose(FactBundle{
		Window:     WindowNow,
		Outlook:    o,
		Now:        &NowFact{BatteryCount: 2},
		Confidence: ConfidenceFact{Register: "hedged"},
	})
	if strings.Contains(n.Outlook, "expensive hours") {
		t.Fatalf("must not claim chart peak: %q", n.Outlook)
	}
	if !strings.Contains(n.Outlook, "discharge window") {
		t.Fatalf("expected discharge window wording: %q", n.Outlook)
	}
}

func TestFormatWindow_SameHourSlot(t *testing.T) {
	start := time.Date(2026, 7, 20, 16, 0, 0, 0, time.Local)
	end := start.Add(15 * time.Minute)
	got := formatWindow(start, end)
	if got != "16:00–16:15" {
		t.Fatalf("got %q", got)
	}
}

func TestComposeNow_BoringIdle(t *testing.T) {
	now := time.Date(2026, 7, 20, 14, 0, 0, 0, time.Local)
	in := Input{
		Window: WindowNow,
		Now:    now,
		Live: LiveScene{
			PVW:   -800,
			LoadW: 700,
			GridW: -100,
			BatW:  0,
		},
	}
	n := Compose(Build(in))
	if len(n.Body) > 2 {
		t.Fatalf("boring moment should stay short, got %d lines: %#v", len(n.Body), n.Body)
	}
	assertTraceable(t, n)
}

func TestComposeToday_MoneyHeadline(t *testing.T) {
	now := time.Date(2026, 7, 20, 18, 0, 0, 0, time.Local)
	in := Input{
		Window:             WindowToday,
		Now:                now,
		HaveMoney:          true,
		SavedOre:           18200,
		BaselineOre:        21300,
		ActualOre:          3100,
		WeekMedianSavedOre: 15000,
		PlanSlots: []PlanSlot{
			{Start: now.Add(-6 * time.Hour), LenMin: 180, Reason: "absorb PV surplus", BatteryW: 4000, PriceOre: 20},
			{Start: now.Add(-2 * time.Hour), LenMin: 120, Reason: "discharge — export at peak", BatteryW: -3500, PriceOre: 130},
		},
	}
	n := Compose(Build(in))
	if !strings.Contains(n.Headline, "182 SEK") {
		t.Fatalf("headline=%q", n.Headline)
	}
	if !strings.Contains(n.Headline, "strong") && !strings.Contains(n.Headline, "typical") {
		t.Fatalf("expected relative framing in headline: %q", n.Headline)
	}
	if n.Action == "" {
		t.Fatal("expected today action from episodes")
	}
	assertTraceable(t, n)
}

func TestCompose_HedgedRegister(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	in := Input{
		Window:          WindowNow,
		Now:             now,
		HaveConfidence:  true,
		PriceConfidence: 0.7,
		PVResidualHigh:  true,
		Live: LiveScene{
			PVW:   -5000,
			LoadW: 2000,
			GridW: -500,
			BatW:  2500,
		},
	}
	n := Compose(Build(in))
	if n.Register != "hedged" {
		t.Fatalf("register=%q want hedged", n.Register)
	}
	if !strings.Contains(n.Action, "cautiously") && n.Restraint == "" {
		t.Fatalf("hedged voice should soften action or add restraint: action=%q restraint=%q", n.Action, n.Restraint)
	}
	assertTraceable(t, n)
}

// assertTraceable ensures every rendered clause has supporting facts.
func assertTraceable(t *testing.T, n Narrative) {
	t.Helper()
	assertNoSignedWatts(t, n)
	if n.Action != "" && n.Facts.Now == nil && len(n.Facts.Episodes) == 0 {
		// Allowed boring fallback still needs Now.
		if !strings.Contains(n.Action, "Holding steady") {
			t.Errorf("action without now/episodes: %q", n.Action)
		}
	}
	if n.Restraint != "" && len(n.Facts.Restraints) == 0 {
		t.Errorf("restraint without facts: %q", n.Restraint)
	}
	if n.Outlook != "" && n.Facts.Outlook == nil {
		t.Errorf("outlook without facts: %q", n.Outlook)
	}
	if n.Headline != "" && n.Facts.Money == nil {
		t.Errorf("headline without money facts: %q", n.Headline)
	}
}

// assertNoSignedWatts: Facts keep site-convention signs; prose uses
// direction words + positive magnitudes (docs/site-convention.md).
func assertNoSignedWatts(t *testing.T, n Narrative) {
	t.Helper()
	for _, s := range []string{n.Headline, n.Action, n.Restraint, n.Outlook} {
		if s == "" {
			continue
		}
		if strings.Contains(s, "~-") {
			t.Errorf("signed watt leaked into prose: %q", s)
			continue
		}
		for _, part := range strings.Fields(s) {
			p := strings.Trim(part, ".,;:")
			if strings.HasPrefix(p, "-") && (strings.HasSuffix(p, "W") || strings.Contains(p, "kW")) {
				t.Errorf("signed watt leaked into prose: %q (token %q)", s, part)
				return
			}
		}
	}
}
