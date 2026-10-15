package agent

import (
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// 本文件是「收官证据门」的工具层(移植 CyberStrikeAI agentfinalizer 的裁决契约:
// 自然语言结论只是候选,证据核实后才允许定稿)。落到 ARTEX 的图模型上,裁决条件 =
// 任务的每个目标都 state='met' 且都有 proves 证据边(证据节点是仍存在的 fact/finding)。
//
// 工具层只管给模型即时反馈(goal_met 拒收、prove_goal 自动收官前复核);真正拦住
// 所有落 done 路径的是 server 侧的 enforceGoalEvidence(server/goal_evidence.go)。

// GoalEvidenceGateEnabled, if set, reports whether the gate is on (server wires
// it from the goal_evidence_gate setting). nil = ON —— 证据门默认开启(fail-closed):
// 宁可先拦下一次没有证据的收官,也不放行。置 false 完全恢复旧行为。
var GoalEvidenceGateEnabled func() bool

// GoalEvidenceGateOn reports whether the goal-evidence gate is enabled (nil → on).
func GoalEvidenceGateOn() bool {
	return GoalEvidenceGateEnabled == nil || GoalEvidenceGateEnabled()
}

// goalEvidenceGapMessage 校验本任务目标的证据齐备度,返回给模型看的拒绝说明。
// 空串 = 通过;err = 校验本身失败(DB 侧),调用方应 fail-closed(拒绝本次收官)。
func goalEvidenceGapMessage(ts *db.ExplorationStore) (string, error) {
	open, unproven, err := ts.GoalEvidenceGap()
	if err != nil {
		return "", err
	}
	if len(open) == 0 && len(unproven) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("证据门未通过，本次不能宣布任务完成：")
	if len(open) > 0 {
		b.WriteString("\n- 仍未达成（open）的目标：" + goalRefsOf(open))
	}
	if len(unproven) > 0 {
		b.WriteString("\n- 已 met 但缺少 proves 证据边的目标：" + goalRefsOf(unproven))
	}
	b.WriteString("\n请先用 prove_goal(goal_id, evidence_id, reason) 为每个目标连上真实证据（事实/漏洞节点）；确实无法达成的目标请说明原因并调整，不要直接收官。")
	return b.String(), nil
}

// goalRefsOf 渲染目标引用列表(「goal 12「文本」」),给拒绝说明用。
func goalRefsOf(ns []*db.Node) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		if txt := db.GoalText(n); txt != "" {
			parts = append(parts, fmt.Sprintf("goal %d「%s」", n.ID, txt))
		} else {
			parts = append(parts, fmt.Sprintf("goal %d", n.ID))
		}
	}
	return strings.Join(parts, "、")
}
