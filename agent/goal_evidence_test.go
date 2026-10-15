package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"

	"github.com/Autumn-27/artex/db"
)

// callToolText 调用一个 CoreTool 并把结果拍平成文本。
func callToolText(t *testing.T, tool actool.CoreTool, payload any) string {
	t.Helper()
	raw, _ := json.Marshal(payload)
	res, err := tool.Call(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("tool call error: %v", err)
	}
	return res.Flatten()
}

// TestGoalMetEvidenceGate 覆盖工具层证据门:
// open 目标被拒 / met 缺证据边被拒 / 有证据边放行 / 开关关闭恢复旧行为。
func TestGoalMetEvidenceGate(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	expID, err := d.CreateExploration("goal-evidence-gate", "证据门工具层测试")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, expID) //nolint:errcheck
	es := d.Exploration(expID)

	// 1) 仍有 open 目标 → goal_met 被拒。
	g1, err := es.AddGoal(map[string]any{"text": "目标一"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	ts := NewToolSet(es, "planner")
	out := callToolText(t, ts.goalMet(), map[string]any{"reason": "测试"})
	if ts.GoalMet {
		t.Fatalf("goal_met must be rejected while a goal is open; out=%s", out)
	}
	if !strings.Contains(out, "证据门") || !strings.Contains(out, "open") {
		t.Fatalf("rejection should explain the gap; out=%s", out)
	}

	// 2) 目标 met 但没有 proves 证据边 → 仍被拒。
	if err := es.SetNodeState(g1, "met"); err != nil {
		t.Fatal(err)
	}
	ts = NewToolSet(es, "planner")
	out = callToolText(t, ts.goalMet(), map[string]any{"reason": "测试"})
	if ts.GoalMet {
		t.Fatalf("goal_met must be rejected without proves edges; out=%s", out)
	}
	if !strings.Contains(out, "proves") {
		t.Fatalf("rejection should mention proves edges; out=%s", out)
	}

	// 3) 补上证据边 → 放行。
	find, err := es.AddNode(db.KindFinding, map[string]any{"vulnclass": "x"}, 9, "confirmed", "worker-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(find, db.RelProves, g1); err != nil {
		t.Fatal(err)
	}
	ts = NewToolSet(es, "planner")
	out = callToolText(t, ts.goalMet(), map[string]any{"reason": "目标已由 finding 证明"})
	if !ts.GoalMet {
		t.Fatalf("goal_met should pass with evidence; out=%s", out)
	}

	// 4) 开关关闭 → 恢复旧行为(open 目标也放行)。
	GoalEvidenceGateEnabled = func() bool { return false }
	t.Cleanup(func() { GoalEvidenceGateEnabled = nil })
	if _, err := es.AddGoal(map[string]any{"text": "目标二"}, "human"); err != nil {
		t.Fatal(err)
	}
	ts = NewToolSet(es, "planner")
	out = callToolText(t, ts.goalMet(), map[string]any{"reason": "测试"})
	if !ts.GoalMet {
		t.Fatalf("gate off should restore legacy behavior; out=%s", out)
	}
	GoalEvidenceGateEnabled = nil
}

// TestProveGoalAutoCompleteEvidenceGate 覆盖 prove_goal 的自动收官复核:最后一个
// 目标被证明时,若更早 met 的目标缺证据边,不自动收官并提示补齐;补齐后自动收官。
func TestProveGoalAutoCompleteEvidenceGate(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	expID, err := d.CreateExploration("goal-evidence-prove", "证据门 prove_goal 测试")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, expID) //nolint:errcheck
	es := d.Exploration(expID)

	// gOld:直接置 met 不建证据边(模拟证据被删/历史库);gNew:open。
	gOld, err := es.AddGoal(map[string]any{"text": "旧目标"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := es.SetNodeState(gOld, "met"); err != nil {
		t.Fatal(err)
	}
	gNew, err := es.AddGoal(map[string]any{"text": "新目标"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	find, err := es.AddNode(db.KindFinding, map[string]any{"vulnclass": "y"}, 9, "confirmed", "worker-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	ts := NewToolSet(es, "planner")
	out := callToolText(t, ts.proveGoal(), map[string]any{"goal_id": gNew, "evidence_id": find, "reason": "证明新目标"})
	if ts.GoalMet {
		t.Fatalf("auto-complete must not fire while goal %d lacks evidence; out=%s", gOld, out)
	}
	if !strings.Contains(out, "proves") || !strings.Contains(out, "缺少") {
		t.Fatalf("message should name the unproven goal; out=%s", out)
	}

	// 给旧目标补证据边 → 再证明一次,应自动收官。
	if err := es.Link(find, db.RelProves, gOld); err != nil {
		t.Fatal(err)
	}
	ts = NewToolSet(es, "planner")
	out = callToolText(t, ts.proveGoal(), map[string]any{"goal_id": gNew, "evidence_id": find, "reason": "证明新目标"})
	if !ts.GoalMet {
		t.Fatalf("auto-complete should fire once every goal has evidence; out=%s", out)
	}
}
