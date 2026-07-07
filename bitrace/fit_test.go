package bitrace

import (
	"math"
	"testing"

	"honnef.co/go/curve"
)

// TestQuadToCubic checks that quadratic-to-cubic degree elevation is exact: the
// elevated cubic must trace the same points as the original quadratic at every
// parameter value t.
func TestQuadToCubic(t *testing.T) {
	p0 := curve.Point{X: 1, Y: 2}
	q := curve.Point{X: 4, Y: -3}
	p2 := curve.Point{X: 7, Y: 5}

	c1, c2 := quadToCubic(p0, q, p2)

	// Known closed forms for the control points.
	wantC1 := curve.Point{X: p0.X + (2.0/3.0)*(q.X-p0.X), Y: p0.Y + (2.0/3.0)*(q.Y-p0.Y)}
	wantC2 := curve.Point{X: p2.X + (2.0/3.0)*(q.X-p2.X), Y: p2.Y + (2.0/3.0)*(q.Y-p2.Y)}
	if math.Abs(c1.X-wantC1.X) > 1e-12 || math.Abs(c1.Y-wantC1.Y) > 1e-12 {
		t.Fatalf("c1 = %v, want %v", c1, wantC1)
	}
	if math.Abs(c2.X-wantC2.X) > 1e-12 || math.Abs(c2.Y-wantC2.Y) > 1e-12 {
		t.Fatalf("c2 = %v, want %v", c2, wantC2)
	}

	// The elevated cubic must coincide with the quadratic pointwise.
	for i := 0; i <= 10; i++ {
		tt := float64(i) / 10.0
		mt := 1 - tt
		// Quadratic B(t) = mt^2*P0 + 2*mt*t*Q + t^2*P2.
		qx := mt*mt*p0.X + 2*mt*tt*q.X + tt*tt*p2.X
		qy := mt*mt*p0.Y + 2*mt*tt*q.Y + tt*tt*p2.Y
		// Cubic B(t) = mt^3*P0 + 3*mt^2*t*C1 + 3*mt*t^2*C2 + t^3*P2.
		cx := mt*mt*mt*p0.X + 3*mt*mt*tt*c1.X + 3*mt*tt*tt*c2.X + tt*tt*tt*p2.X
		cy := mt*mt*mt*p0.Y + 3*mt*mt*tt*c1.Y + 3*mt*tt*tt*c2.Y + tt*tt*tt*p2.Y
		if math.Abs(cx-qx) > 1e-12 || math.Abs(cy-qy) > 1e-12 {
			t.Errorf("t=%.1f: cubic (%v,%v) != quad (%v,%v)", tt, cx, cy, qx, qy)
		}
	}
}

// TestAppendElement drives the per-element command conversion for every path
// element kind, including the QuadTo case that FitToBezPath never emits but that
// must still elevate correctly.
func TestAppendElement(t *testing.T) {
	start := curve.Point{X: 0, Y: 0}
	cur := start

	var cmds []Command
	cmds = appendElement(cmds, curve.MoveTo(start), &cur) // no command, sets cur
	if len(cmds) != 0 || cur != start {
		t.Fatalf("MoveTo: cmds=%v cur=%v", cmds, cur)
	}

	line := curve.Point{X: 1, Y: 1}
	cmds = appendElement(cmds, curve.LineTo(line), &cur)
	if cur != line || cmds[len(cmds)-1].Kind != LineTo {
		t.Fatalf("LineTo: cur=%v last=%v", cur, cmds[len(cmds)-1])
	}

	// QuadTo from the current point (1,1) with control (2,4) to end (5,1).
	qc := curve.Point{X: 2, Y: 4}
	qe := curve.Point{X: 5, Y: 1}
	preQuad := cur
	cmds = appendElement(cmds, curve.QuadTo(qc, qe), &cur)
	last := cmds[len(cmds)-1]
	wantC1, wantC2 := quadToCubic(preQuad, qc, qe)
	if last.Kind != CubicTo {
		t.Fatalf("QuadTo: expected CubicTo, got %v", last.Kind)
	}
	if last.C1 != pt(wantC1) || last.C2 != pt(wantC2) || last.P != pt(qe) {
		t.Fatalf("QuadTo elevation wrong: got C1=%v C2=%v P=%v", last.C1, last.C2, last.P)
	}
	if cur != qe {
		t.Fatalf("QuadTo: cur=%v want %v", cur, qe)
	}

	// CubicTo passes control points through unchanged.
	cc1 := curve.Point{X: 6, Y: 2}
	cc2 := curve.Point{X: 7, Y: 3}
	ce := curve.Point{X: 8, Y: 0}
	cmds = appendElement(cmds, curve.CubicTo(cc1, cc2, ce), &cur)
	last = cmds[len(cmds)-1]
	if last.Kind != CubicTo || last.C1 != pt(cc1) || last.C2 != pt(cc2) || last.P != pt(ce) || cur != ce {
		t.Fatalf("CubicTo passthrough wrong: %v cur=%v", last, cur)
	}

	// ClosePath is ignored and emits no command.
	before := len(cmds)
	cmds = appendElement(cmds, curve.ClosePath(), &cur)
	if len(cmds) != before {
		t.Fatalf("ClosePath should emit nothing, got %d new commands", len(cmds)-before)
	}
}
