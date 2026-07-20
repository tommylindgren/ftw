package narrative

import "strings"

// Kind is a closed taxonomy mapped from planner reason strings and live
// scene facts. The composer never freestyles new kinds.
type Kind string

const (
	KindAbsorbPV       Kind = "absorb_pv"
	KindChargeGrid     Kind = "charge_grid"
	KindCharge         Kind = "charge"
	KindDischargePeak  Kind = "discharge_peak"
	KindDischargeCover Kind = "discharge_cover"
	KindDischarge      Kind = "discharge"
	KindIdleImport     Kind = "idle_import"
	KindIdleExport     Kind = "idle_export"
	KindIdle           Kind = "idle"
	KindCurtail        Kind = "curtail"
	KindEVDominates    Kind = "ev_dominates"
	KindUnknown        Kind = "unknown"
)

// KindFromReason maps mpc.reasonFor (and curtail annotations) onto Kind.
func KindFromReason(reason string) Kind {
	r := strings.ToLower(strings.TrimSpace(reason))
	if r == "" {
		return KindUnknown
	}
	if strings.Contains(r, "curtail") {
		return KindCurtail
	}
	switch {
	case strings.Contains(r, "absorb pv"):
		return KindAbsorbPV
	case strings.Contains(r, "charge from cheap grid"), strings.Contains(r, "charge — import"), strings.Contains(r, "charge - import"):
		return KindChargeGrid
	case strings.HasPrefix(r, "charge"):
		return KindCharge
	case strings.Contains(r, "export at peak"), strings.Contains(r, "price above horizon"):
		return KindDischargePeak
	case strings.Contains(r, "cover local load"):
		return KindDischargeCover
	case strings.HasPrefix(r, "discharge"):
		return KindDischarge
	case strings.Contains(r, "idle — import"), strings.Contains(r, "idle - import"):
		return KindIdleImport
	case strings.Contains(r, "idle — export"), strings.Contains(r, "idle - export"):
		return KindIdleExport
	case strings.HasPrefix(r, "idle"):
		return KindIdle
	default:
		return KindUnknown
	}
}
