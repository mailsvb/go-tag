package meter

import (
	"math"
	"testing"
	"time"
)

func TestMeterTake(t *testing.T) {
	var m Meter
	m.Process([]float32{0.5, -0.25, -0.5, 0.25, 1.5, 0}, 2)
	r := m.Take()
	if r.Peak[0] != 1.5 || r.Peak[1] != 0.25 || r.Frames != 3 || r.Clips == 0 {
		t.Errorf("reading = %+v", r)
	}
	if r := m.Take(); r.Peak[0] != 0 || r.Frames != 0 {
		t.Errorf("Take did not reset: %+v", r)
	}
	if DB(1) != 0 || math.Abs(DB(0.5)+6.02) > 0.01 {
		t.Errorf("DB")
	}
}

func TestBallistics(t *testing.T) {
	b := NewBallistics()
	t0 := time.Unix(0, 0)
	step := 100 * time.Millisecond
	b.Update(Reading{Peak: [MaxChannels]float64{1, 1}}, t0, step) // 0 dB: instant attack
	if b.Level[0] != 0 || b.Hold[0] != 0 || b.Max[0] != 0 {
		t.Fatalf("attack: %+v", b)
	}
	now := t0
	for i := 0; i < 10; i++ { // 1 s of silence
		now = now.Add(step)
		b.Update(Reading{}, now, step)
	}
	if math.Abs(b.Level[0]+20) > 0.01 {
		t.Errorf("level after 1 s = %v, want -20 (20 dB/s release)", b.Level[0])
	}
	if b.Hold[0] != 0 {
		t.Errorf("hold released before HoldTime: %v", b.Hold[0])
	}
	for i := 0; i < 10; i++ {
		now = now.Add(step)
		b.Update(Reading{}, now, step)
	}
	if b.Hold[0] >= 0 || b.Max[0] != 0 {
		t.Errorf("after hold: hold %v, max %v", b.Hold[0], b.Max[0])
	}
	b.Reset()
	if b.Max[0] != -120 {
		t.Errorf("Reset")
	}
}
