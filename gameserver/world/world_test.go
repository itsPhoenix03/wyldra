package world

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"testing"
)

// These are the digests produced by the JavaScript reference implementation
// (shared/world.js) via tools/worldparity/digest.mjs. They pin the Go port to
// bit-exact parity: if a future edit changes the generated world at all, this
// test fails.
//
// Re-derive with:  node tools/worldparity/compare.mjs
var hearth1 = map[string]string{
	"tiles":     "2e4485b66349b49a667d75711092f74862f9e2bc31bc1dc82bfd763b999b305f",
	"elev":      "c3c9026a3d93a36a319c2ffe2891088aadea92760ae55c6bd92a743321602a6c",
	"veins":     "de376adc851095ed23e40aae7fb76a8458f243551e8717eb7a648186679161c1",
	"waterTemp": "00432e309b72cac108471b9b1a97a8f7408032875fafd45c2170a25ef906cd7d",
	"tileVis":   "1a9f83daa2edf16edf31edc91cb86ab98d6230b3184b247df3d5fdd14e95af83",
	"nodes":     "98d9bd3d4d9b0fc6683149d6b04768fc9d8b52322bd9363efe080a5f9a2aeba1",
	"bergs":     "4ad66ba13b3409ab76e0d5ce0dcfa169509adbb7fb38fae6ffcee90fcde70c8d",
	"decor":     "badfe9ac1d48857f65e2ca95ba65cc895ad313d9fe2bfdacb358409b41c7f331",
}

const (
	hearth1Nodes = 4102
	hearth1Bergs = 1043
	hearth1Decor = 171
)

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// joined mirrors the JS `arr.join('\n') + '\n'` used by digest.mjs.
func joined(lines []string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestGenWorldHearth1Digest(t *testing.T) {
	w := GenWorld("hearth-1")

	for name, arr := range map[string][]uint8{
		"tiles": w.Tiles, "elev": w.Elev, "veins": w.Veins,
		"waterTemp": w.WaterTemp, "tileVis": w.TileVis,
	} {
		if len(arr) != SIZE*SIZE {
			t.Fatalf("%s: length = %d, want %d", name, len(arr), SIZE*SIZE)
		}
		if got := sha(arr); got != hearth1[name] {
			t.Errorf("%s digest = %s, want %s (world generation drifted from shared/world.js)", name, got, hearth1[name])
		}
	}

	if len(w.Nodes) != hearth1Nodes {
		t.Errorf("nodes count = %d, want %d", len(w.Nodes), hearth1Nodes)
	}
	if len(w.Bergs) != hearth1Bergs {
		t.Errorf("bergs count = %d, want %d", len(w.Bergs), hearth1Bergs)
	}
	if len(w.Decor) != hearth1Decor {
		t.Errorf("decor count = %d, want %d", len(w.Decor), hearth1Decor)
	}

	var nl []string
	for _, i := range SortedNodeKeys(w.Nodes) {
		nl = append(nl, strconv.Itoa(i)+" "+strconv.Itoa(int(w.Nodes[i])))
	}
	if got := sha(joined(nl)); got != hearth1["nodes"] {
		t.Errorf("nodes digest = %s, want %s", got, hearth1["nodes"])
	}

	var bl []string
	for _, i := range SortedBergKeys(w.Bergs) {
		bl = append(bl, strconv.Itoa(i))
	}
	if got := sha(joined(bl)); got != hearth1["bergs"] {
		t.Errorf("bergs digest = %s, want %s", got, hearth1["bergs"])
	}

	var dl []string
	for _, i := range SortedDecorKeys(w.Decor) {
		dl = append(dl, strconv.Itoa(i)+" "+w.Decor[i])
	}
	if got := sha(joined(dl)); got != hearth1["decor"] {
		t.Errorf("decor digest = %s, want %s", got, hearth1["decor"])
	}
}

// TestAleaKnownValues pins alea@1.0.1's output for the seed used by genWorld.
// Reference values from: alea('hearth-1e') in Node.
func TestAleaKnownValues(t *testing.T) {
	want := []float64{
		0.13823944795876741,
		0.9355960583779961,
		0.13693887717090547,
		0.020699897315353155,
		0.20401701563969254,
	}
	a := NewAlea("hearth-1e")
	for i, w := range want {
		if got := a.Next(); got != w {
			t.Errorf("Next()[%d] = %v, want %v", i, got, w)
		}
	}
}

// TestNoise2DKnownValues pins simplex-noise@4.0.3 output.
// Reference values from: createNoise2D(alea('hearth-1e')) in Node.
func TestNoise2DKnownValues(t *testing.T) {
	n := NewNoise2D(NewAlea("hearth-1e"))
	cases := []struct {
		x, y, want float64
	}{
		{0, 0, 0},
		{1.0 / 28, 1.0 / 28, 0.15308554548343936},
		{37.5, -12.25, 0.6239538314618094},
		{900, 900, -0.6850322974610329},
	}
	for _, c := range cases {
		if got := n.Eval(c.x, c.y); got != c.want {
			t.Errorf("Eval(%v, %v) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// TestToInt32IsTruncationNotFloor guards the `t | 0` semantics: JS truncates
// toward zero, so -3.7|0 == -3 while Math.floor(-3.7) == -4.
func TestToInt32IsTruncationNotFloor(t *testing.T) {
	if got := toInt32(-3.7); got != -3 {
		t.Errorf("toInt32(-3.7) = %v, want -3", got)
	}
	if got := toUint32(-1); got != 4294967295 {
		t.Errorf("toUint32(-1) = %v, want 4294967295", got)
	}
	if got := toInt32(4294967296 + 5); got != 5 {
		t.Errorf("toInt32(2^32+5) = %v, want 5", got)
	}
}

// TestJSHypot2MatchesJSHypot: the generator's two-argument hypot must be the
// variadic V8 port bit for bit, including signed zeros and wild magnitudes.
func TestJSHypot2MatchesJSHypot(t *testing.T) {
	a := NewAlea("hypot")
	vals := []float64{0, math.Copysign(0, -1), 1, -1, 3, 4, 1e-300, 1e300, 0.1, 7.5}
	for i := 0; i < 20000; i++ {
		vals = append(vals, (a.Next()-0.5)*math.Pow(10, float64(int(a.Next()*12)-6)))
	}
	for i := 0; i < len(vals); i++ {
		x, y := vals[i], vals[(i*7+3)%len(vals)]
		if g, w := jsHypot2(x, y), jsHypot(x, y); math.Float64bits(g) != math.Float64bits(w) {
			t.Fatalf("jsHypot2(%v, %v) = %v, jsHypot = %v", x, y, g, w)
		}
	}
}

// TestClearingsMirrorFolkFires: the village clearings are levelled where
// folk.go seeks each village fire — the two must move together.
func TestClearingsMirrorFolkFires(t *testing.T) {
	for _, v := range folkVillageDefs {
		k := -1
		for i, m := range MajorIslands {
			if m.ID == v.island {
				k = i
			}
		}
		want := [2]int{ISLES[k][0] + v.off[0], ISLES[k][1] + v.off[1]}
		found := false
		for _, c := range Clearings {
			if c.ID == v.island+"-village" {
				found = true
				if c.K != k || [2]int{c.X, c.Y} != want {
					t.Errorf("%s: clearing at %v (island %d), folk fire sought at %v (island %d)", c.ID, [2]int{c.X, c.Y}, c.K, want, k)
				}
			}
		}
		if !found {
			t.Errorf("no %s-village clearing", v.island)
		}
	}
}

// BenchmarkGenWorld times one full world:
//
//	go test -run '^$' -bench GenWorld -benchtime 5x ./world
func BenchmarkGenWorld(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenWorld("hearth-1")
	}
}
