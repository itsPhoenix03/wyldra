package hosting

import (
	"reflect"
	"testing"
)

// HEARTH_FEATURES sets the process-wide flag defaults; a world's registry entry
// sets its own over them (room/features.go has the precedence, the save last).
func TestFeatureFlagsFromEnvAndRegistry(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_FEATURES", "tileMountains=0, cameraPeek=1, nonsense=1")
	t.Setenv("HEARTH_WORLDS", `[
		{"worldId":"plain","seed":"s","instanceId":"local"},
		{"worldId":"peaks","seed":"s","instanceId":"local","features":{"tileMountains":true}}
	]`)
	var logged []string
	cfg, err := LoadConfig(func(f string, a ...any) { logged = append(logged, f) })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Features, map[string]bool{"tileMountains": false, "cameraPeek": true}) {
		t.Fatalf("HEARTH_FEATURES parsed as %v", cfg.Features)
	}
	if len(logged) == 0 {
		t.Fatal("an unknown flag in HEARTH_FEATURES was not reported")
	}
	plain, _ := cfg.World("plain")
	peaks, _ := cfg.World("peaks")
	if got := cfg.WorldFeatures(plain); got["tileMountains"] || !got["cameraPeek"] {
		t.Fatalf("plain world features = %v", got)
	}
	if got := cfg.WorldFeatures(peaks); !got["tileMountains"] || !got["cameraPeek"] {
		t.Fatalf("a world's own features did not win over HEARTH_FEATURES: %v", got)
	}
}
