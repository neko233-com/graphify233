package export

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/neko233-com/graphify233/internal/model"
)

// MaxNodesForViz mirrors the reference cap for the interactive viewer.
const MaxNodesForViz = 5000

// ErrTooLarge reports a graph that exceeds the interactive viewer's budget. It
// is not fatal: the pipeline skips graph.html and keeps every other artifact.
var ErrTooLarge = errors.New("graph too large for the interactive viewer")

type vizNode struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	C     int     `json:"c"`
	T     string  `json:"t"`
	F     string  `json:"f"`
	Loc   string  `json:"loc"`
	Deg   int     `json:"deg"`
	Size  float64 `json:"size"`
}

type vizLink struct {
	S    int32  `json:"s"`
	T    int32  `json:"t"`
	Rel  string `json:"rel"`
	Conf string `json:"conf"`
}

type vizCommunity struct {
	Name     string  `json:"name"`
	Cohesion float64 `json:"cohesion"`
	Color    string  `json:"color"`
}

type vizPayload struct {
	Nodes       []vizNode      `json:"nodes"`
	Links       []vizLink      `json:"links"`
	Communities []vizCommunity `json:"communities"`
	Stats       struct {
		Nodes       int `json:"nodes"`
		Edges       int `json:"edges"`
		Communities int `json:"communities"`
	} `json:"stats"`
	Root string `json:"root"`
}

// WriteHTML renders a fully self-contained interactive viewer (no CDN, no
// network access) using a Canvas 2D renderer.
func WriteHTML(path string, g *model.Graph, comms [][]int32, labels map[int]string, cohesion []float64, root string) error {
	if len(g.Nodes) > MaxNodesForViz {
		return fmt.Errorf("%w: %d nodes (limit %d)", ErrTooLarge, len(g.Nodes), MaxNodesForViz)
	}
	pts := Layout(g, comms)

	payload := vizPayload{Root: root}
	payload.Stats.Nodes = len(g.Nodes)
	payload.Stats.Edges = len(g.Edges)
	payload.Stats.Communities = len(comms)
	payload.Nodes = make([]vizNode, len(g.Nodes))
	maxDeg := 1
	for i := range g.Nodes {
		if d := g.Degree(int32(i)); d > maxDeg {
			maxDeg = d
		}
	}
	for i := range g.Nodes {
		deg := g.Degree(int32(i))
		n := g.Nodes[i]
		label := n.Label
		ftype := n.FileType
		payload.Nodes[i] = vizNode{
			ID: n.ID, Label: label, X: round1(pts[i].X), Y: round1(pts[i].Y),
			C: n.Community, T: ftype, F: n.SourceFile, Loc: n.SourceLocation,
			Deg: deg, Size: round1(4 + 7*math.Sqrt(float64(deg)/float64(maxDeg))),
		}
	}
	for _, e := range g.Edges {
		s := g.Index(e.Source)
		t := g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		payload.Links = append(payload.Links, vizLink{S: s, T: t, Rel: e.Relation, Conf: e.Confidence})
	}
	payload.Communities = make([]vizCommunity, len(comms))
	for ci := range comms {
		name := labels[ci]
		if name == "" {
			name = fmt.Sprintf("Community %d", ci)
		}
		var co float64
		if ci < len(cohesion) {
			co = cohesion[ci]
		}
		payload.Communities[ci] = vizCommunity{Name: name, Cohesion: round2(co), Color: communityColor(ci)}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	w.WriteString(htmlHead)
	w.Write(data)
	w.WriteString(htmlTail)
	return w.Flush()
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

func communityColor(i int) string {
	h := math.Mod(float64(i)*137.508, 360)
	return fmt.Sprintf("hsl(%.0f, 62%%, 56%%)", h)
}

const htmlHead = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>graphify233 - knowledge graph</title>
<style>
  html,body{margin:0;height:100%;background:#0d1117;color:#e6edf3;font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;overflow:hidden}
  #wrap{position:fixed;inset:0}
  canvas{display:block;width:100%;height:100%;cursor:grab}
  canvas.dragging{cursor:grabbing}
  #hud{position:fixed;left:14px;top:14px;max-width:360px;background:rgba(22,27,34,.92);border:1px solid #30363d;border-radius:10px;padding:12px 14px;backdrop-filter:blur(6px)}
  #hud h1{font-size:15px;margin:0 0 6px 0;letter-spacing:.2px}
  #hud .stats{color:#8b949e;font-size:12px}
  #search{width:100%;box-sizing:border-box;margin-top:10px;padding:7px 9px;border-radius:7px;border:1px solid #30363d;background:#0d1117;color:#e6edf3;outline:none}
  #search:focus{border-color:#58a6ff}
  #legend{position:fixed;left:14px;bottom:14px;max-height:42vh;overflow:auto;background:rgba(22,27,34,.92);border:1px solid #30363d;border-radius:10px;padding:10px 12px;font-size:12px;min-width:180px}
  #legend .row{display:flex;align-items:center;gap:8px;margin:3px 0;cursor:pointer;white-space:nowrap}
  #legend .dot{width:10px;height:10px;border-radius:50%;flex:0 0 auto}
  #legend .nm{overflow:hidden;text-overflow:ellipsis;max-width:230px}
  #legend .ct{color:#8b949e;margin-left:auto}
  #info{position:fixed;right:14px;top:14px;width:300px;max-height:60vh;overflow:auto;background:rgba(22,27,34,.94);border:1px solid #30363d;border-radius:10px;padding:12px 14px;display:none}
  #info h2{font-size:14px;margin:0 0 4px}
  #info .meta{color:#8b949e;font-size:12px;margin-bottom:8px;word-break:break-all}
  #info .lnk{color:#8b949e;font-size:12px;padding:2px 0;border-bottom:1px solid #21262d}
  #info b{color:#e6edf3;font-weight:600}
  #tip{position:fixed;pointer-events:none;background:#161b22;border:1px solid #30363d;padding:5px 8px;border-radius:6px;font-size:12px;display:none;max-width:320px}
  #help{position:fixed;right:14px;bottom:14px;color:#8b949e;font-size:11px;text-align:right}
</style>
</head>
<body>
<div id="wrap"><canvas id="cv"></canvas></div>
<div id="hud">
  <h1>Knowledge Graph</h1>
  <div class="stats" id="line1"></div>
  <div class="stats" id="line2"></div>
  <input id="search" placeholder="Search nodes..." autocomplete="off">
</div>
<div id="legend"></div>
<div id="info"></div>
<div id="tip"></div>
<div id="help">drag to pan · wheel to zoom · click a node · Esc to reset</div>
<script>const DATA = `

const htmlTail = `;
(function(){
  const cv = document.getElementById('cv');
  const ctx = cv.getContext('2d');
  const nodes = DATA.nodes, links = DATA.links, comms = DATA.communities;
  let dpr = Math.max(1, window.devicePixelRatio || 1);
  const view = {x:0, y:0, k:1};
  let hover = -1, selected = -1, filter = '', legendOff = new Set();
  const maxDeg = nodes.reduce((m,n)=>Math.max(m,n.deg),1);

  document.getElementById('line1').textContent = DATA.stats.nodes + ' nodes · ' + DATA.stats.edges + ' edges';
  document.getElementById('line2').textContent = DATA.stats.communities + ' communities · ' + DATA.root;

  function resize(){
    const w = window.innerWidth, h = window.innerHeight;
    cv.width = Math.floor(w*dpr); cv.height = Math.floor(h*dpr);
    cv.style.width = w+'px'; cv.style.height = h+'px';
    draw();
  }
  window.addEventListener('resize', resize);

  function fit(){
    let minx=1e9,miny=1e9,maxx=-1e9,maxy=-1e9;
    for(const n of nodes){ if(n.x<minx)minx=n.x; if(n.x>maxx)maxx=n.x; if(n.y<miny)miny=n.y; if(n.y>maxy)maxy=n.y; }
    const w = window.innerWidth-80, h = window.innerHeight-80;
    const k = Math.min(w/Math.max(1,maxx-minx), h/Math.max(1,maxy-miny));
    view.k = Math.min(2.5, Math.max(0.15, k));
    view.x = 40 - minx*view.k + (w-(maxx-minx)*view.k)/2;
    view.y = 40 - miny*view.k + (h-(maxy-miny)*view.k)/2;
  }

  function toScreen(x,y){ return [x*view.k+view.x, y*view.k+view.y]; }
  function toGraph(px,py){ return [(px-view.x)/view.k, (py-view.y)/view.k]; }

  function colorOf(n){ return comms[n.c] ? comms[n.c].color : '#8b949e'; }

  function draw(){
    const W = cv.width/dpr, H = cv.height/dpr;
    ctx.setTransform(dpr,0,0,dpr,0,0);
    ctx.fillStyle = '#0d1117'; ctx.fillRect(0,0,W,H);
    const q = filter.trim().toLowerCase();
    const shown = n => !legendOff.has(n.c);

    // edges
    ctx.lineWidth = 1;
    for(const e of links){
      const a = nodes[e.s], b = nodes[e.t];
      if(!a || !b || !shown(a) || !shown(b)) continue;
      let alpha = e.conf === 'EXTRACTED' ? 0.30 : (e.conf === 'INFERRED' ? 0.16 : 0.08);
      if(selected >= 0 && e.s !== selected && e.t !== selected) alpha *= 0.25;
      if(hover >= 0 && e.s !== hover && e.t !== hover && selected < 0) alpha *= 0.5;
      const [x1,y1] = toScreen(a.x,a.y), [x2,y2] = toScreen(b.x,b.y);
      ctx.strokeStyle = 'rgba(139,148,158,'+alpha+')';
      ctx.beginPath(); ctx.moveTo(x1,y1); ctx.lineTo(x2,y2); ctx.stroke();
    }
    // nodes
    for(let i=0;i<nodes.length;i++){
      const n = nodes[i];
      if(!shown(n)) continue;
      const [x,y] = toScreen(n.x,n.y);
      if(x < -40 || y < -40 || x > W+40 || y > H+40) continue;
      let r = Math.max(2.5, n.size * Math.sqrt(view.k));
      const matched = q && n.label.toLowerCase().includes(q);
      ctx.globalAlpha = (q && !matched) ? 0.18 : 1;
      ctx.beginPath(); ctx.arc(x,y,r,0,6.2832);
      ctx.fillStyle = colorOf(n); ctx.fill();
      if(i === hover || i === selected || matched){
        ctx.lineWidth = 2; ctx.strokeStyle = '#ffffff'; ctx.stroke();
      }
      ctx.globalAlpha = 1;
      if(view.k > 0.9 || n.deg >= maxDeg*0.25 || matched){
        if(!q || matched || n.deg >= maxDeg*0.25){
          ctx.font = '11px -apple-system,Segoe UI,Roboto,sans-serif';
          ctx.fillStyle = 'rgba(230,237,243,.92)';
          ctx.fillText(n.label, x + r + 3, y + 3.5);
        }
      }
    }
  }

  function hitTest(px,py){
    let best = -1, bestD = 12*12;
    for(let i=0;i<nodes.length;i++){
      const n = nodes[i];
      const [x,y] = toScreen(n.x,n.y);
      const dx = x-px, dy = y-py;
      const d = dx*dx+dy*dy;
      if(d < bestD){ bestD = d; best = i; }
    }
    return best;
  }

  // interactions
  let dragging = false, moved = false, lastX = 0, lastY = 0;
  cv.addEventListener('mousedown', e => { dragging = true; moved = false; lastX = e.clientX; lastY = e.clientY; cv.classList.add('dragging'); });
  window.addEventListener('mouseup', () => { dragging = false; cv.classList.remove('dragging'); });
  cv.addEventListener('mousemove', e => {
    if(dragging){
      const dx = e.clientX-lastX, dy = e.clientY-lastY;
      if(Math.abs(dx)+Math.abs(dy) > 2) moved = true;
      view.x += dx; view.y += dy; lastX = e.clientX; lastY = e.clientY;
      draw(); return;
    }
    const h = hitTest(e.clientX, e.clientY);
    if(h !== hover){ hover = h; draw(); }
    const tip = document.getElementById('tip');
    if(h >= 0){
      const n = nodes[h];
      tip.style.display = 'block';
      tip.style.left = (e.clientX+14)+'px'; tip.style.top = (e.clientY+14)+'px';
      tip.innerHTML = '<b>'+esc(n.label)+'</b><br>'+esc(n.t||'')+' · '+esc(n.f||'')+' '+esc(n.loc||'')+'<br>'+n.deg+' connections';
    } else { tip.style.display = 'none'; }
  });
  cv.addEventListener('mouseleave', () => { document.getElementById('tip').style.display='none'; hover=-1; draw(); });
  cv.addEventListener('click', e => {
    if(moved) return;
    const h = hitTest(e.clientX, e.clientY);
    selected = h;
    showInfo(h);
    draw();
  });
  cv.addEventListener('wheel', e => {
    e.preventDefault();
    const [gx,gy] = toGraph(e.clientX, e.clientY);
    const factor = Math.exp(-e.deltaY*0.0015);
    view.k = Math.min(8, Math.max(0.05, view.k*factor));
    view.x = e.clientX - gx*view.k; view.y = e.clientY - gy*view.k;
    draw();
  }, {passive:false});

  document.addEventListener('keydown', e => {
    if(e.key === 'Escape'){ selected = -1; filter=''; document.getElementById('search').value=''; document.getElementById('info').style.display='none'; fit(); draw(); }
    if(e.key === 'f' || e.key === 'F'){ fit(); draw(); }
  });

  document.getElementById('search').addEventListener('input', e => { filter = e.target.value; draw(); });

  function esc(s){ return String(s).replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }

  function showInfo(i){
    const box = document.getElementById('info');
    if(i < 0){ box.style.display = 'none'; return; }
    const n = nodes[i];
    const neigh = [];
    for(const e of links){
      if(e.s === i) neigh.push([e.t, e.rel, e.conf, 'out']);
      else if(e.t === i) neigh.push([e.s, e.rel, e.conf, 'in']);
    }
    neigh.sort((a,b)=>a[1].localeCompare(b[1]));
    let html = '<h2>'+esc(n.label)+'</h2><div class="meta">'+esc(n.t||'')+' · '+esc(n.f||'')+' '+esc(n.loc||'')+'<br>'+(n.deg)+' connections · community '+n.c+'</div>';
    for(const [j,rel,conf,dir] of neigh.slice(0,60)){
      html += '<div class="lnk">'+esc(rel)+' · '+esc(conf)+' · <b>'+esc(nodes[j].label)+'</b></div>';
    }
    if(neigh.length > 60) html += '<div class="lnk">(+'+(neigh.length-60)+' more)</div>';
    box.innerHTML = html; box.style.display = 'block';
  }

  // legend
  const legend = document.getElementById('legend');
  const byComm = {};
  for(const n of nodes){ (byComm[n.c] = byComm[n.c] || []).push(n); }
  const order = Object.keys(byComm).map(Number).sort((a,b)=>byComm[b].length-byComm[a].length);
  for(const c of order){
    const row = document.createElement('div');
    row.className = 'row';
    const co = comms[c];
    row.innerHTML = '<span class="dot" style="background:'+(co?co.color:'#8b949e')+'"></span><span class="nm">'+(co?esc(co.name):('community '+c))+'</span><span class="ct">'+byComm[c].length+'</span>';
    row.addEventListener('click', () => {
      if(legendOff.has(c)) legendOff.delete(c); else legendOff.add(c);
      row.style.opacity = legendOff.has(c) ? '0.35' : '1';
      draw();
    });
    legend.appendChild(row);
  }

  fit(); resize();
})();
</script>
</body>
</html>
`
