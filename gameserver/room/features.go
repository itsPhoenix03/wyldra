package room

import (
	"fmt"
	"sort"
	"strings"
)

// Feature flags: per-room, server-authoritative switches for features that can
// be turned off without a rebuild.
//
// A flag's value lives on the room, never on a client: `init` carries the whole
// set as `features` and a `feat` broadcast carries it again whenever a tester
// changes one (dev.go `devcmd feat`), so every client in the world always draws
// the same world. The values come from, in order of precedence:
//
//  1. the room's own save (persist.Snapshot.Features) — what a tester last set;
//  2. the world's registry entry (`features` in control/worlds.json / HEARTH_WORLDS);
//  3. HEARTH_FEATURES, e.g. `HEARTH_FEATURES="tileMountains=0,cameraPeek=1"`;
//  4. the registry default below.
//
// hosting folds 2 and 3 into Config.Features; the save then wins over that, so
// a switch a tester flipped survives a restart. A save written before flags
// existed has no `features` and loads with the defaults, unchanged.
//
// Adding a flag is one line in featureDefs. Terrain flags change the ground
// itself; flipping one runs applyTerrainFeatures, which stamps or lifts the
// change and re-streams the chunks it touched to everyone connected.

// featureDef is one registered flag.
type featureDef struct {
	Name    string
	Default bool
	Desc    string
	// Terrain: changing it changes the world's tiles, so the chunks it touches
	// are rebuilt and re-pushed (applyTerrainFeatures).
	Terrain bool
}

// featureDefs is the registry, in the order the tester panel lists them.
var featureDefs = []featureDef{
	{"tileMountains", true, "Mountains built from stacked tiles at the five mountain sites (gameserver/world/mountains.go), instead of the sprite mountains", true},
	{"cameraPeek", true, "The camera leans toward hidden ground and height changes while the keeper moves (each player can also turn it off in Settings)", false},
}

// FeatureNames lists every registered flag.
func FeatureNames() []string {
	out := make([]string, len(featureDefs))
	for i, d := range featureDefs {
		out[i] = d.Name
	}
	return out
}

func findFeature(name string) (featureDef, bool) {
	for _, d := range featureDefs {
		if d.Name == name {
			return d, true
		}
	}
	return featureDef{}, false
}

// ParseFeatures reads a HEARTH_FEATURES-style list: `name=value` pairs
// separated by commas, value one of 1/0, true/false, on/off, yes/no. A pair
// naming an unknown flag, or with a value that is none of those, is skipped and
// reported in problems — a typo in an env var is worth a log line, not a
// refusal to boot.
func ParseFeatures(spec string) (flags map[string]bool, problems []string) {
	flags = map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, val, ok := strings.Cut(part, "=")
		name, val = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(val))
		if _, known := findFeature(name); !known {
			problems = append(problems, fmt.Sprintf("unknown feature %q (known: %s)", name, strings.Join(FeatureNames(), ", ")))
			continue
		}
		switch {
		case ok && (val == "1" || val == "true" || val == "on" || val == "yes"):
			flags[name] = true
		case ok && (val == "0" || val == "false" || val == "off" || val == "no"):
			flags[name] = false
		default:
			problems = append(problems, fmt.Sprintf("feature %q: want %s=1 or %s=0, got %q", name, name, name, part))
		}
	}
	return flags, problems
}

// initFeatures sets every flag to its default, then to the configured value
// where one is given. Unknown names are ignored (hosting already reported them).
func (r *Room) initFeatures(configured map[string]bool) {
	r.features = make(map[string]bool, len(featureDefs))
	for _, d := range featureDefs {
		r.features[d.Name] = d.Default
		if v, ok := configured[d.Name]; ok {
			r.features[d.Name] = v
		}
	}
}

// applySavedFeatures takes the flags a save recorded. Absent (nil) keeps the
// configured values; a name this build does not know is dropped.
func (r *Room) applySavedFeatures(saved map[string]bool) {
	for name, v := range saved {
		if _, known := findFeature(name); known {
			r.features[name] = v
		}
	}
}

// feature reports one flag.
func (r *Room) feature(name string) bool { return r.features[name] }

// featuresWire is the whole flag set as `init.features` / `feat.features` and
// the save carry it. A fresh map: callers marshal or store it.
func (r *Room) featuresWire() map[string]bool {
	out := make(map[string]bool, len(r.features))
	for k, v := range r.features {
		out[k] = v
	}
	return out
}

// setFeature changes one flag and does whatever the change needs: a terrain
// flag re-stamps the ground and re-streams its chunks, then everyone is told.
// It reports whether anything changed.
func (r *Room) setFeature(name string, on bool) bool {
	d, known := findFeature(name)
	if !known || r.features[name] == on {
		return false
	}
	r.features[name] = on
	// the flags first, then the ground: a client that hears tileMountains go
	// off hides nothing it still needs, and the re-pushed chunks follow at once
	r.broadcast(map[string]any{"t": "feat", "features": r.featuresWire()})
	if d.Terrain {
		r.applyTerrainFeatures(true)
	}
	return true
}

// handleFeatCmd is `devcmd {cmd:"feat", name, on}` from the F10 tester panel
// (dev.go has already checked the dev claim). The name must be a registered
// flag and `on` a JSON boolean; a no-op is refused quietly.
func (r *Room) handleFeatCmd(p *Player, m map[string]any) {
	name := getString(m, "name")
	d, known := findFeature(name)
	if !known {
		known := FeatureNames()
		sort.Strings(known)
		r.send(p, map[string]any{"t": "msg", "s": fmt.Sprintf("⚑ No feature flag %q (there are: %s)", name, strings.Join(known, ", "))})
		return
	}
	on, isBool := m["on"].(bool)
	if !isBool || !r.setFeature(name, on) {
		return
	}
	state := "OFF"
	if on {
		state = "ON"
	}
	tail := ""
	if d.Terrain {
		tail = " — the ground was rebuilt"
	}
	r.send(p, map[string]any{"t": "msg", "s": fmt.Sprintf("⚑ %s %s%s", d.Name, state, tail)})
	r.cfg.Logger.Printf("[hearth] %s (%s) set feature %s=%v", p.S.ID, p.Name, name, on)
}
