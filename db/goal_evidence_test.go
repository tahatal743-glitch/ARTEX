package db

import "testing"

// TestGoalEvidenceGap 覆盖证据门数据层的判定:open 目标、有证据的 met 目标、
// 缺证据的 met 目标(proves 源不是 fact/finding、证据节点被删两种成因),以及
// 跨任务证据节点(prove_goal 允许引用直接关联任务的节点,边写在本探索)。
func TestGoalEvidenceGap(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	expID, err := d.CreateExploration("goal-evidence", "证据门数据层测试")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, expID) //nolint:errcheck
	es := d.Exploration(expID)

	gOpen, err := es.AddGoal(map[string]any{"text": "未达成目标"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	gProven, err := es.AddGoal(map[string]any{"text": "有证据目标"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	gUnproven, err := es.AddGoal(map[string]any{"text": "缺证据目标"}, "human")
	if err != nil {
		t.Fatal(err)
	}

	// finding proves gProven → 有证据;intent(非 fact/finding)proves gUnproven → 不算证据。
	find, err := es.AddNode(KindFinding, map[string]any{"vulnclass": "x", "severity": "high"}, 9, "confirmed", "worker-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(find, RelProves, gProven); err != nil {
		t.Fatal(err)
	}
	intent, err := es.AddIntent(map[string]any{"summary": "枚举"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(intent, RelProves, gUnproven); err != nil {
		t.Fatal(err)
	}
	if err := es.SetNodeState(gProven, "met"); err != nil {
		t.Fatal(err)
	}
	if err := es.SetNodeState(gUnproven, "met"); err != nil {
		t.Fatal(err)
	}

	open, unproven, err := es.GoalEvidenceGap()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != gOpen {
		t.Fatalf("open goals: want [%d], got %v", gOpen, nodeIDs(open))
	}
	if len(unproven) != 1 || unproven[0].ID != gUnproven {
		t.Fatalf("unproven goals: want [%d], got %v", gUnproven, nodeIDs(unproven))
	}

	// 跨任务证据:另一个探索的 finding 节点 proves 本探索的目标 → 视为有证据。
	exp2, err := d.CreateExploration("goal-evidence-src", "证据来源任务")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, exp2) //nolint:errcheck
	es2 := d.Exploration(exp2)
	foreign, err := es2.AddNode(KindFinding, map[string]any{"vulnclass": "y"}, 9, "confirmed", "worker-2", nil)
	if err != nil {
		t.Fatal(err)
	}
	gCross, err := es.AddGoal(map[string]any{"text": "跨任务证据目标"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(foreign, RelProves, gCross); err != nil {
		t.Fatal(err)
	}
	if err := es.SetNodeState(gCross, "met"); err != nil {
		t.Fatal(err)
	}
	if _, unproven, err = es.GoalEvidenceGap(); err != nil || len(unproven) != 1 {
		t.Fatalf("cross-task evidence should count as proven: unproven=%v err=%v", nodeIDs(unproven), err)
	}

	// 证据节点被删 → 边随外键级联消失,目标落回 unproven。
	if _, err := d.Exec(`DELETE FROM exploration_nodes WHERE id=$1`, foreign); err != nil {
		t.Fatal(err)
	}
	_, unproven, err = es.GoalEvidenceGap()
	if err != nil {
		t.Fatal(err)
	}
	if len(unproven) != 2 {
		t.Fatalf("after evidence deletion: want 2 unproven (gUnproven+gCross), got %v", nodeIDs(unproven))
	}

	// 按 prove_goal 的写法补一条证据边 → 只剩被删证据的 gCross。
	fact, err := es.AddNode(KindFact, map[string]any{"text": "确认"}, 5, "confirmed", "worker-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(fact, RelProves, gUnproven); err != nil {
		t.Fatal(err)
	}
	if _, unproven, err = es.GoalEvidenceGap(); err != nil || len(unproven) != 1 || unproven[0].ID != gCross {
		t.Fatalf("after re-proving: want only gCross unproven, got %v err=%v", nodeIDs(unproven), err)
	}
}

func nodeIDs(ns []*Node) []int64 {
	out := make([]int64, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}
