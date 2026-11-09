package analyze

import (
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func node(id, label, file, loc string) model.Node {
	return model.Node{ID: id, Label: label, FileType: model.FileTypeCode, SourceFile: file, SourceLocation: loc, Community: -1}
}

func edge(s, t, rel, conf, file string) model.Edge {
	w := 1.0
	if conf == model.ConfInferred {
		w = 0.8
	}
	return model.Edge{Source: s, Target: t, Relation: rel, Confidence: conf, SourceFile: file, Weight: w}
}

func starGraph() *model.Graph {
	g := model.New()
	for _, n := range []model.Node{
		node("a", "a.py", "a.py", "L1"),
		node("a_hub", "hub()", "a.py", "L3"),
		node("a_one", "one()", "a.py", "L5"),
		node("a_two", "two()", "a.py", "L7"),
		node("a_three", "three()", "a.py", "L9"),
		node("b", "b.py", "b.py", "L1"),
		node("b_leaf", "leaf()", "b.py", "L3"),
	} {
		g.AddNode(n)
	}
	for _, e := range []model.Edge{
		edge("a", "a_hub", "contains", model.ConfExtracted, "a.py"),
		edge("a", "a_one", "contains", model.ConfExtracted, "a.py"),
		edge("a", "a_two", "contains", model.ConfExtracted, "a.py"),
		edge("a", "a_three", "contains", model.ConfExtracted, "a.py"),
		edge("a_one", "a_hub", "calls", model.ConfInferred, "a.py"),
		edge("a_two", "a_hub", "calls", model.ConfInferred, "a.py"),
		edge("a_three", "a_hub", "calls", model.ConfInferred, "a.py"),
		edge("a_hub", "b_leaf", "calls", model.ConfAmbiguous, "a.py"),
		edge("b", "b_leaf", "contains", model.ConfExtracted, "b.py"),
	} {
		g.AddEdge(e)
	}
	return g
}

func TestGodNodesSkipsFileNodesAndSortsByDegree(t *testing.T) {
	g := starGraph()
	gods := GodNodes(g, 3)
	if len(gods) == 0 {
		t.Fatal("no god nodes")
	}
	if gods[0].Label != "hub()" {
		t.Fatalf("top god node = %q, want hub()", gods[0].Label)
	}
	for _, gn := range gods {
		if model.IsFileLabel(gn.Label) {
			t.Fatalf("file node %q should not be a god node", gn.Label)
		}
	}
}

func TestSurprisingConnectionsCrossFile(t *testing.T) {
	g := starGraph()
	comms := [][]int32{{0, 1, 2, 3, 4}, {5, 6}}
	surprises := SurprisingConnections(g, comms, 5)
	if len(surprises) == 0 {
		t.Fatal("expected at least one surprising connection")
	}
	found := false
	for _, s := range surprises {
		if s.Source == "a_hub" && s.Target == "b_leaf" {
			found = true
			if len(s.Why) == 0 {
				t.Fatal("surprise has no explanation")
			}
			if s.Confidence != model.ConfAmbiguous {
				t.Fatalf("confidence = %q, want AMBIGUOUS", s.Confidence)
			}
		}
	}
	if !found {
		t.Fatalf("cross-file surprise missing: %+v", surprises)
	}
}

func TestLabelCommunitiesAndQuestions(t *testing.T) {
	g := starGraph()
	comms := [][]int32{{0, 1, 2, 3, 4}, {5, 6}}
	labels := LabelCommunities(g, comms)
	if len(labels) != 2 {
		t.Fatalf("got %d labels, want 2", len(labels))
	}
	for id, l := range labels {
		if l == "" {
			t.Fatalf("community %d has empty label", id)
		}
	}
	q := SuggestedQuestions(g, comms, CohesionScores(g, comms), labels)
	if len(q) == 0 {
		t.Fatal("no suggested questions")
	}
}
