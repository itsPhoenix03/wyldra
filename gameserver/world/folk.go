package world

import (
	"math"
	"strconv"
)

// Island villages: settlements of non-player folk (villagers, hunters,
// children and an elder in each) living in huts round one shared fire. Like the
// medics, a village's tiles are a deterministic function of the world, so
// every restart puts it in the same place — except that a tile in `reserved`
// (medic tiles, the spawn, player structures already in the save) is never
// chosen, so an old base is never built over.

// FolkCamp is one hut of a village: its hut tile, the village fire it shares
// with the other huts, and the roster of folk sprite keys who live there.
type FolkCamp struct {
	ID         string
	IslandID   string
	HutSprite  string
	HutX, HutY int
	FireX      int
	FireY      int
	Folk       []string
	// YardR is how far from the fire the village's yard reaches.
	YardR float64
}

type folkVillageDef struct {
	island string
	off    [2]int     // where the village fire is sought, from the island centre
	hut    string     // hut sprite
	ring   float64    // how far the huts stand from the fire
	huts   [][]string // residents per hut; the elder lives in the first
}

// folkVillageDefs: the Woods is the Hearthfolk's home and the largest village;
// the Dunes is the Ashmark hunters' (the only place they live), a little
// smaller, led by their shaman; the Marsh is three reed huts of Elder Yvenne's
// tribal folk (the only place they live); the Spire is too harsh for more than
// one hut beside its medic's (world.go MedicIslands).
var folkVillageDefs = []folkVillageDef{
	{"woods", [2]int{-8, 7}, "folk_hut", 6, [][]string{
		{"elder_woods", "villager2"},
		{"villager", "villager2", "woods_child"},
		{"villager", "villager2"},
		{"villager", "woods_child"},
		{"villager", "villager2", "woods_child"},
		{"villager", "villager2"},
	}},
	{"dunes", [2]int{-8, -9}, "folk_hut_dunes", 5.5, [][]string{
		{"shaman", "ashmark_hunter"},
		{"ashmark_hunter", "villager2", "villager"},
		{"ashmark_hunter", "villager"},
		{"ashmark_hunter", "villager2"},
		{"villager", "villager2"},
	}},
	{"marsh", [2]int{9, -9}, "folk_hut_marsh", 4.5, [][]string{
		{"elder_yvenne", "tribal"},
		{"tribal", "villager2"},
		{"tribal", "villager"},
	}},
	{"spire", [2]int{4, 12}, "folk_hut_spire", 3, [][]string{
		{"elder_spire", "villager_snow"},
	}},
}

// CampBlockTiles are the tiles a camp occupies: the hut and the fire. The door
// tile in front of the hut is left open — folk walk through it.
func CampBlockTiles(c FolkCamp) [2]int {
	return [2]int{c.HutY*SIZE + c.HutX, c.FireY*SIZE + c.FireX}
}

// FindFolkCamps places every village it can, one FolkCamp per hut. A village
// with no site for its fire, or a hut with no room near its spot on the ring,
// is skipped rather than failing the world: folk are ambience, not progression.
func FindFolkCamps(w *World, reserved map[int]bool) []FolkCamp {
	var out []FolkCamp
	for _, v := range folkVillageDefs {
		var island MajorIsland
		for _, cand := range MajorIslands {
			if cand.ID == v.island {
				island = cand
			}
		}
		cx, cy := island.Center[0], island.Center[1]
		taken := map[int]bool{} // this village's fire, huts and yards so far
		open := func(x, y int) bool {
			if x < 0 || y < 0 || x >= SIZE || y >= SIZE {
				return false
			}
			i := y*SIZE + x
			_, hasNode := w.Nodes[i]
			return w.Tiles[i] == island.Tile && !hasNode && !LandmarkBlock[i] && !reserved[i] && !taken[i]
		}
		// the fire: open ground with a clear tile all round it
		fx, fy, ok := FindNearestValidTile(cx+v.off[0], cy+v.off[1], func(x, y, _ int) bool {
			if jsHypot(float64(x-cx), float64(y-cy)) <= 5 {
				return false
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if !open(x+dx, y+dy) {
						return false
					}
				}
			}
			return true
		}, 30)
		if !ok {
			continue
		}
		taken[fy*SIZE+fx] = true
		huts := map[int]bool{}  // hut tiles so far
		paths := map[int]bool{} // the walks from the fire to their doors
		// path: the tiles of a straight walk from the fire to the door of a hut at
		// (hutX, hutY), or nil if water, a landmark, a hut or reserved ground is in
		// the way — folk steer straight for their door, so it must be clear
		path := func(hutX, hutY int) []int {
			x0, y0 := float64(fx)+.5, float64(fy)+.5
			x1, y1 := float64(hutX)+1.5, float64(hutY)+1.5
			n := int(jsHypot(x1-x0, y1-y0)/0.25) + 1
			var tiles []int
			for k := 1; k <= n; k++ {
				t := float64(k) / float64(n)
				x, y := int(x0+(x1-x0)*t), int(y0+(y1-y0)*t)
				i := y*SIZE + x
				if x == fx && y == fy {
					continue
				}
				if w.Tiles[i] == TWater || LandmarkBlock[i] || reserved[i] || huts[i] || (x == hutX && y == hutY) {
					return nil
				}
				tiles = append(tiles, i)
			}
			return tiles
		}
		// apart: a hut at (x, y) would not overlap one already placed on screen — the
		// art is 96×88 px, and a tile step moves it 32 px across or 16 px down
		var placed [][2]int
		apart := func(x, y int) bool {
			for _, h := range placed {
				if abs((x-y)-(h[0]-h[1])) < 3 && abs((x+y)-(h[0]+h[1])) < 6 {
					return false
				}
			}
			return true
		}
		for k, folk := range v.huts {
			// huts evenly round the fire, the elder's straight behind it
			a := float64(k)*2*math.Pi/float64(len(v.huts)) - 3*math.Pi/4
			tx := fx + int(math.Round(math.Cos(a)*v.ring))
			ty := fy + int(math.Round(math.Sin(a)*v.ring))
			hx, hy, ok := FindNearestValidTile(tx, ty, func(x, y, i int) bool {
				// the hut, its door and a yard in front of it are all open ground, clear of
				// the fire and the other huts' paths, with a clear walk from the fire to the door
				return max(abs(x-fx), abs(y-fy)) >= 3 && !paths[i] && apart(x, y) &&
					open(x, y) && open(x+1, y+1) && open(x+2, y+1) && open(x+1, y+2) &&
					path(x, y) != nil
			}, 8)
			if !ok {
				continue
			}
			huts[hy*SIZE+hx] = true
			placed = append(placed, [2]int{hx, hy})
			for _, i := range path(hx, hy) {
				paths[i] = true
			}
			// keep the next hut off this one, its door and its yard
			for dy := -1; dy <= 2; dy++ {
				for dx := -1; dx <= 2; dx++ {
					taken[(hy+dy)*SIZE+hx+dx] = true
				}
			}
			out = append(out, FolkCamp{
				ID: "camp-" + v.island + "-" + strconv.Itoa(k), IslandID: v.island, HutSprite: v.hut,
				HutX: hx, HutY: hy, FireX: fx, FireY: fy,
				Folk:  append([]string(nil), folk...),
				YardR: v.ring + 3,
			})
		}
	}
	return out
}
