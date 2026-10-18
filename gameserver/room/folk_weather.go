package room

// Folk and the weather (docs/10_GO_WIRE_PROTOCOL.md §13.5).
//
// Weather is one global state that bites per biome (weather.go): a sandstorm
// flays the Dunes, a snowstorm freezes the Spire, rain falls on the Woods and
// the Marsh. The folk of the island it is blowing on react the moment it
// starts, like the keeper has to:
//
//	sandstorm / snowstorm  everyone, watchers too, heads in (`shelter`) and stays
//	                       in until it passes — out there it is deadly
//	rain                   the children are called in; the adults carry on
//
// Folk elsewhere carry on: a sandstorm is nothing to the Woods.

// stormDrivesIn reports whether the weather now blowing sends f indoors.
func (r *Room) stormDrivesIn(f *Folk) bool {
	if f.vil == nil {
		return false
	}
	switch r.weather.kind {
	case "sandstorm":
		return f.vil.isle == "dunes"
	case "snowstorm":
		return f.vil.isle == "spire"
	case "rain":
		return f.child && (f.vil.isle == "woods" || f.vil.isle == "marsh")
	}
	return false
}
