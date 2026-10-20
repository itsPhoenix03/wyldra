package room

import (
	"fmt"
	"math"
	"testing"

	"hearth/gameserver/world"
)

// The folk tick's cost (folk.go). Every village's folk live a day while the
// benchmark runs: each op is one tick, and the clock moves on a tick at a
// time, so the number is the average over mornings, meals, evenings and the
// night watch. The budget for the whole sim tick is 200 ms; the folk should be
// a sliver of it.

func folkBenchRoom(b *testing.B) *Room {
	b.Helper()
	if sharedWorld == nil {
		sharedWorld = world.GenWorld("hearth-1")
	}
	r, err := New(Config{Seed: "hearth-1", World: sharedWorld})
	if err != nil {
		b.Fatal(err)
	}
	now := int64(1_700_000_000_000)
	r.nowFn = func() int64 { return now }
	s := NewSession("bench00", "u00", "Bench", "t")
	go func() {
		for range s.Out {
		}
	}()
	p := newPlayer(s, r.spawn, now, r.defs)
	p.X, p.Y = 640, 600
	r.players[s.ID] = p
	r.playerOrder = append(r.playerOrder, p)
	return r
}

func benchFolkDay(b *testing.B, r *Room) {
	step := (TickMS / 1000.0) / DayLengthSec
	r.time = 0.3
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.tickN++
		was := r.time
		if r.time = math.Mod(r.time+step, 1); r.time < was {
			r.day++
		}
		r.folkTick()
	}
}

// BenchmarkFolkTick: every village going about its days, no keeper near.
func BenchmarkFolkTick(b *testing.B) {
	benchFolkDay(b, folkBenchRoom(b))
}

// BenchmarkFolkTickKeepers: four keepers walking round the Woods yard, so the
// folk turn to them, wave, talk and give them room.
func BenchmarkFolkTickKeepers(b *testing.B) {
	r := folkBenchRoom(b)
	v := r.villageOf("woods")
	for n := range 3 {
		s := NewSession(fmt.Sprintf("bench%02d", n+1), fmt.Sprintf("u%02d", n+1), "Bench", "t")
		go func() {
			for range s.Out {
			}
		}()
		p := newPlayer(s, r.spawn, r.now(), r.defs)
		r.players[s.ID] = p
		r.playerOrder = append(r.playerOrder, p)
	}
	k := 0
	step := (TickMS / 1000.0) / DayLengthSec
	r.time = 0.3
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k++
		for n, q := range r.playerOrder {
			a := float64(k)*0.01 + float64(n)*1.6
			q.X, q.Y = v.fx+math.Cos(a)*(3+float64(n)), v.fy+math.Sin(a)*(3+float64(n))
		}
		r.tickN++
		was := r.time
		if r.time = math.Mod(r.time+step, 1); r.time < was {
			r.day++
		}
		r.folkTick()
	}
}

// BenchmarkFolkTickAlarm: a crawler prowling round the Woods and the Dunes —
// guards out, everyone else running in and coming back out.
func BenchmarkFolkTickAlarm(b *testing.B) {
	r := folkBenchRoom(b)
	var cs []*Creature
	for _, isle := range []string{"woods", "dunes"} {
		v := r.villageOf(isle)
		cs = append(cs, r.newCreature(v.fx+12, v.fy, 10, "crawler", 0, false))
	}
	step := (TickMS / 1000.0) / DayLengthSec
	r.time = 0.3
	k := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k++
		for n, c := range cs {
			v := r.villageOf([]string{"woods", "dunes"}[n])
			a := float64(k) * 0.003
			rad := 9 + 8*math.Sin(float64(k)*0.002) // in and out of the alarm
			c.X, c.Y = v.fx+math.Cos(a)*rad, v.fy+math.Sin(a)*rad
		}
		r.tickN++
		r.time = math.Mod(r.time+step, 1)
		r.folkTick()
	}
}
