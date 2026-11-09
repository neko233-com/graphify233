package export

import (
	"math"
	"sort"

	"github.com/neko233-com/graphify233/internal/model"
)

// Point is a 2D layout position in graph coordinates.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Layout places communities on a ring and their nodes on golden-angle spirals.
// It is O(n log n), deterministic, and produces the clustered look of the
// reference viewer without a physics simulation.
func Layout(g *model.Graph, comms [][]int32) []Point {
	n := len(g.Nodes)
	pts := make([]Point, n)
	if n == 0 {
		return pts
	}
	nc := len(comms)
	if nc == 0 {
		placeSpiral(g, pts, nil)
		return normalize(pts, 40, 40, 1560, 960)
	}

	// Order communities by size (already sorted) but lay them out by angle.
	ringRadius := 120.0
	total := 0
	for _, c := range comms {
		total += len(c)
	}
	ringRadius = 140 * math.Sqrt(float64(total)) / math.Sqrt(float64(nc))
	if ringRadius < 240 {
		ringRadius = 240
	}
	if nc == 1 {
		ringRadius = 0
	}

	for ci, c := range comms {
		ang := 2 * math.Pi * float64(ci) / float64(nc)
		cx := ringRadius * math.Cos(ang)
		cy := ringRadius * math.Sin(ang)
		placeCluster(g, pts, c, cx, cy)
	}
	return normalize(pts, 40, 40, 1560, 960)
}

func placeCluster(g *model.Graph, pts []Point, members []int32, cx, cy float64) {
	if len(members) == 0 {
		return
	}
	// Highest-degree nodes near the center of their cluster.
	ordered := make([]int32, len(members))
	copy(ordered, members)
	sort.SliceStable(ordered, func(i, j int) bool {
		di, dj := g.Degree(ordered[i]), g.Degree(ordered[j])
		if di != dj {
			return di > dj
		}
		return ordered[i] < ordered[j]
	})
	radius := 24 + 18*math.Sqrt(float64(len(ordered)))
	golden := math.Pi * (3 - math.Sqrt(5))
	for k, m := range ordered {
		frac := math.Sqrt(float64(k) / float64(len(ordered)))
		r := radius * frac
		a := float64(k)*golden + float64(len(members))*0.7
		pts[m] = Point{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}
}

func placeSpiral(g *model.Graph, pts []Point, ordered []int32) {
	_ = g
	golden := math.Pi * (3 - math.Sqrt(5))
	for k := range pts {
		r := 30 * math.Sqrt(float64(k+1))
		a := float64(k) * golden
		pts[k] = Point{X: r * math.Cos(a), Y: r * math.Sin(a)}
	}
}

func normalize(pts []Point, x0, y0, x1, y1 float64) []Point {
	if len(pts) == 0 {
		return pts
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	}
	w := maxX - minX
	h := maxY - minY
	if w == 0 {
		w = 1
	}
	if h == 0 {
		h = 1
	}
	sx := (x1 - x0) / w
	sy := (y1 - y0) / h
	for i := range pts {
		pts[i] = Point{X: x0 + (pts[i].X-minX)*sx, Y: y0 + (pts[i].Y-minY)*sy}
	}
	return pts
}
