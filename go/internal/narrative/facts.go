package narrative

import "time"

// Window selects which story surface to compose.
type Window string

const (
	WindowNow   Window = "now"
	WindowToday Window = "today"
	WindowDay   Window = "day"
)

// Input is everything the API layer gathers before calling Build+Compose.
// Pure data — no I/O inside the package.
type Input struct {
	Window Window
	Now    time.Time

	Live LiveScene

	// PlanSlots are upcoming (and optionally recent) planner actions.
	PlanSlots []PlanSlot

	LastReplanReason string
	LastReplanAt     time.Time

	// Money for today/day windows (öre). Optional for now.
	SavedOre           float64
	BaselineOre        float64
	ActualOre          float64
	WeekMedianSavedOre float64
	HaveMoney          bool

	// Twin / price confidence hooks.
	PriceConfidence float64 // 1 = day-ahead; <1 predicted. 0 = unknown.
	PVResidualHigh  bool    // planner was cautious on PV forecast
	HaveConfidence  bool
}

// LiveScene is the ENERGY BALANCE snapshot operators stare at.
type LiveScene struct {
	PVW      float64
	LoadW    float64
	GridW    float64
	BatW     float64
	EVW      float64
	BatSoC   float64
	FusePct  float64 // 0–100+; 0 means unknown
	HaveFuse bool

	// BatteryCount is online home-battery packs. Used for singular/plural
	// prose ("battery" vs "batteries"). 0 means unknown → singular.
	BatteryCount int

	// BatTargetW is the sum of last dispatch charge(+)/discharge(−) setpoints
	// for home batteries. Used to narrate target-vs-actual gaps (e.g. target
	// 500 W while measured idle).
	BatTargetW float64

	EVPluggedIn    bool
	EVTargetSoCPct float64
	EVDeadline     time.Time
	EVName         string // charger / loadpoint label for prose
}

// PlanSlot is a thin view of mpc.Action for narrative use.
type PlanSlot struct {
	Start      time.Time
	LenMin     int
	Reason     string
	BatteryW   float64
	GridW      float64
	PriceOre   float64
	PVW        float64
	LoadW      float64
	LoadpointW float64
	SoCPct     float64
	Confidence float64
}

// FactBundle is the machine-truth graph behind every sentence.
type FactBundle struct {
	Window     Window          `json:"window"`
	Now        *NowFact        `json:"now,omitempty"`
	Episodes   []Episode       `json:"episodes,omitempty"`
	Restraints []RestraintFact `json:"restraints,omitempty"`
	Money      *MoneyFacts     `json:"money,omitempty"`
	Replans    []ReplanEvent   `json:"replans,omitempty"`
	Intents    []IntentFact    `json:"intents,omitempty"`
	Confidence ConfidenceFact  `json:"confidence"`
	Outlook    *OutlookFact    `json:"outlook,omitempty"`
}

type NowFact struct {
	Kind         Kind    `json:"kind"`
	PVW          float64 `json:"pv_w"`
	LoadW        float64 `json:"load_w"`
	GridW        float64 `json:"grid_w"`
	BatW         float64 `json:"bat_w"`
	BatTargetW   float64 `json:"bat_target_w,omitempty"`
	EVW          float64 `json:"ev_w"`
	BatSoC       float64 `json:"bat_soc"`
	BatteryCount int     `json:"battery_count"`
	FusePct      float64 `json:"fuse_pct,omitempty"`
	Importing    bool    `json:"importing"`
	EVDominates  bool    `json:"ev_dominates"`
	// TargetLag is true when dispatch wants meaningful charge/discharge
	// but measured battery power is still near idle.
	TargetLag bool `json:"target_lag,omitempty"`
}

type Episode struct {
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	Kind           Kind      `json:"kind"`
	DominantReason string    `json:"dominant_reason"`
	EnergyKWh      float64   `json:"energy_kwh"`
	MeanPriceOre   float64   `json:"mean_price_ore"`
}

type RestraintFact struct {
	WouldHave string `json:"would_have"`
	Instead   string `json:"instead"`
	Because   string `json:"because"`
}

type MoneyFacts struct {
	SavedOre           float64 `json:"saved_ore"`
	BaselineOre        float64 `json:"baseline_ore"`
	ActualOre          float64 `json:"actual_ore"`
	WeekMedianSavedOre float64 `json:"week_median_saved_ore"`
	Relative           string  `json:"relative"` // strong_week | typical | weak_week | ""
}

type ReplanEvent struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

type IntentFact struct {
	Kind      string    `json:"kind"` // ev_deadline | away
	TargetSoC float64   `json:"target_soc,omitempty"`
	Deadline  time.Time `json:"deadline,omitempty"`
	Label     string    `json:"label,omitempty"`
}

type ConfidenceFact struct {
	Register        string  `json:"register"` // assertive | hedged
	PriceConfidence float64 `json:"price_confidence,omitempty"`
	PVResidualHigh  bool    `json:"pv_residual_high,omitempty"`
}

type OutlookFact struct {
	Kind     Kind      `json:"kind"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	PriceOre float64   `json:"price_ore"`
	SoCPct   float64   `json:"soc_pct"`
	Reason   string    `json:"reason"`
}

// Narrative is the composed English prose plus the facts that justify it.
type Narrative struct {
	Headline  string     `json:"headline,omitempty"`
	Action    string     `json:"action,omitempty"`
	Restraint string     `json:"restraint,omitempty"`
	Outlook   string     `json:"outlook,omitempty"`
	Body      []string   `json:"body"`
	Register  string     `json:"register"`
	Facts     FactBundle `json:"facts"`
}
