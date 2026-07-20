package api

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/srcfl/ftw/go/internal/mpc"
	"github.com/srcfl/ftw/go/internal/narrative"
	"github.com/srcfl/ftw/go/internal/state"
	"github.com/srcfl/ftw/go/internal/telemetry"
)

// handleNarrative composes the Live "energy story" from live status,
// planner slots, and (for today/day) savings totals.
//
// GET /api/narrative?window=now|today|day&date=YYYY-MM-DD
func (s *Server) handleNarrative(w http.ResponseWriter, r *http.Request) {
	window := narrative.Window(r.URL.Query().Get("window"))
	switch window {
	case narrative.WindowNow, narrative.WindowToday, narrative.WindowDay:
	default:
		window = narrative.WindowNow
	}

	now := time.Now()
	in := narrative.Input{
		Window: window,
		Now:    now,
		Live:   s.narrativeLiveScene(),
	}

	if s.deps.MPC != nil {
		if plan := s.deps.MPC.Latest(); plan != nil {
			in.PlanSlots = narrativePlanSlots(plan.Actions)
			var sum, n float64
			for _, a := range plan.Actions {
				if a.Confidence > 0 {
					sum += a.Confidence
					n++
				}
			}
			if n > 0 {
				in.PriceConfidence = sum / n
				in.HaveConfidence = true
			}
		}
		at, reason := s.deps.MPC.LastReplanInfo()
		in.LastReplanAt = at
		in.LastReplanReason = reason
	}

	if s.deps.PVModel != nil {
		rd := s.deps.PVModel.ResidualDiagSnapshot()
		m := s.deps.PVModel.Model()
		if m.RatedW > 0 && rd.StdW > 0.15*m.RatedW {
			in.PVResidualHigh = true
			in.HaveConfidence = true
		}
	}

	if window == narrative.WindowToday || window == narrative.WindowDay {
		s.fillNarrativeMoney(&in, r.URL.Query().Get("date"), now)
	}

	writeJSON(w, 200, narrative.Compose(narrative.Build(in)))
}

func narrativePlanSlots(actions []mpc.Action) []narrative.PlanSlot {
	out := make([]narrative.PlanSlot, 0, len(actions))
	for _, a := range actions {
		lenMin := a.SlotLenMin
		if lenMin <= 0 {
			lenMin = 15
		}
		out = append(out, narrative.PlanSlot{
			Start:      time.UnixMilli(a.SlotStartMs),
			LenMin:     lenMin,
			Reason:     a.Reason,
			BatteryW:   a.BatteryW,
			GridW:      a.GridW,
			PriceOre:   a.PriceOre,
			PVW:        a.PVW,
			LoadW:      a.LoadW,
			LoadpointW: a.LoadpointW,
			SoCPct:     a.SoCPct,
			Confidence: a.Confidence,
		})
	}
	return out
}

func (s *Server) narrativeLiveScene() narrative.LiveScene {
	live := narrative.LiveScene{}
	if s.deps.Tel == nil || s.deps.Ctrl == nil {
		return live
	}

	s.deps.CtrlMu.Lock()
	ctrl := *s.deps.Ctrl
	s.deps.CtrlMu.Unlock()

	gridW := 0.0
	haveGrid := false
	if statusDriverTelemetryUsable(s.deps.Tel, ctrl.SiteMeterDriver) {
		if r := s.deps.Tel.Get(ctrl.SiteMeterDriver, telemetry.DerMeter); r != nil {
			gridW = r.SmoothedW
			haveGrid = true
		}
	}
	var pvW, batW float64
	batCount := 0
	for _, r := range s.deps.Tel.ReadingsByType(telemetry.DerPV) {
		if !statusDriverOnline(s.deps.Tel, r.Driver) {
			continue
		}
		pvW += r.SmoothedW
	}
	for _, r := range s.deps.Tel.ReadingsByType(telemetry.DerBattery) {
		if !statusDriverOnline(s.deps.Tel, r.Driver) {
			continue
		}
		batW += r.SmoothedW
		batCount++
	}
	evW := s.deps.Tel.SumOnlineEVW()
	v2xW := s.deps.Tel.SumOnlineV2XW()
	loadW := 0.0
	if haveGrid {
		loadW = gridW - batW - pvW - evW - v2xW
		if loadW < 0 {
			loadW = 0
		}
	}

	s.deps.CapMu.RLock()
	caps := make(map[string]float64, len(s.deps.Capacities))
	for k, v := range s.deps.Capacities {
		caps[k] = v
	}
	s.deps.CapMu.RUnlock()
	var totalCap, weightedSoC float64
	for _, b := range s.deps.Tel.ReadingsByType(telemetry.DerBattery) {
		if !statusDriverOnline(s.deps.Tel, b.Driver) {
			continue
		}
		cap, ok := caps[b.Driver]
		if !ok {
			continue
		}
		totalCap += cap
		soc := 0.0
		if b.SoC != nil {
			soc = *b.SoC
		}
		weightedSoC += soc * cap
	}
	if totalCap > 0 {
		live.BatSoC = weightedSoC / totalCap
	}

	live.PVW = pvW
	live.LoadW = loadW
	live.GridW = gridW
	live.BatW = batW
	live.EVW = evW
	live.BatteryCount = batCount

	for _, t := range ctrl.LastTargets {
		live.BatTargetW += t.TargetW
	}

	s.deps.CfgMu.RLock()
	maxAmps := 0.0
	if s.deps.Cfg != nil {
		maxAmps = s.deps.Cfg.Fuse.MaxAmps
	}
	s.deps.CfgMu.RUnlock()
	phases := siteMeterPhaseAmps(s.deps.Tel, ctrl.SiteMeterDriver)
	if maxAmps > 0 && len(phases) > 0 {
		peak := 0.0
		for _, a := range phases {
			if math.Abs(a) > peak {
				peak = math.Abs(a)
			}
		}
		live.FusePct = peak / maxAmps * 100
		live.HaveFuse = true
	}

	if s.deps.Loadpoints != nil {
		for _, st := range s.deps.Loadpoints.States() {
			if !st.PluggedIn {
				continue
			}
			live.EVPluggedIn = true
			live.EVTargetSoCPct = st.TargetSoCPct
			live.EVDeadline = st.TargetTime
			if st.DriverName != "" {
				live.EVName = st.DriverName
			} else {
				live.EVName = st.ID
			}
			break
		}
	}
	return live
}

func (s *Server) fillNarrativeMoney(in *narrative.Input, date string, now time.Time) {
	if s.deps.State == nil {
		return
	}
	zone := ""
	ep := state.ExportPricing{}
	if s.deps.CfgMu != nil && s.deps.Cfg != nil {
		s.deps.CfgMu.RLock()
		if s.deps.Cfg.Price != nil {
			zone = s.deps.Cfg.Price.Zone
			ep.BonusOreKwh = s.deps.Cfg.Price.ExportBonusOreKwh
			ep.FeeOreKwh = s.deps.Cfg.Price.ExportFeeOreKwh
			ep.FloorOreKwh = s.deps.Cfg.Price.ExportFloorOreKwh
		}
		if s.deps.Cfg.Planner != nil {
			ep.FlatOreKwh = s.deps.Cfg.Planner.ExportOrePerKWh
		}
		s.deps.CfgMu.RUnlock()
	}
	if zone == "" {
		return
	}

	loc := now.Location()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd := now
	if in.Window == narrative.WindowDay {
		if date != "" {
			if t, err := time.ParseInLocation("2006-01-02", date, loc); err == nil {
				dayStart = t
				dayEnd = t.AddDate(0, 0, 1)
			}
		} else {
			dayEnd = dayStart.AddDate(0, 0, 1)
		}
	}

	b, err := s.deps.State.DailyCostBreakdown(dayStart.UnixMilli(), dayEnd.UnixMilli(), zone, ep)
	if err != nil {
		return
	}
	in.HaveMoney = true
	in.SavedOre = b.SavedOre()
	in.BaselineOre = b.BaselineCostOre
	in.ActualOre = b.ActualCostOre()

	var saved []float64
	for i := 1; i <= 7; i++ {
		ds := dayStart.AddDate(0, 0, -i)
		de := ds.AddDate(0, 0, 1)
		db, err := s.deps.State.DailyCostBreakdown(ds.UnixMilli(), de.UnixMilli(), zone, ep)
		if err != nil || db.PriceSlotCount == 0 {
			continue
		}
		saved = append(saved, db.SavedOre())
	}
	if len(saved) > 0 {
		sort.Float64s(saved)
		in.WeekMedianSavedOre = saved[len(saved)/2]
	}
}
