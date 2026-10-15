package server

import (
	"fmt"
	"log"
	"strings"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
)

// enforceGoalEvidence 是任务收官前的证据门(移植 CyberStrikeAI agentfinalizer 的
// 裁决契约:自然语言结论只是候选,证据核实后才允许定稿)。裁决条件 = 每个目标都
// state='met' 且都有 proves 证据边(源是仍存在的 fact/finding)。三处落 done 的
// 路径都必须过这道门:planner 判 met(engine.go 的 case met)、goalless 分支前沿
// 抽干(engine.go)、超时收尾的终局判定(engine_timeout.go)。
//
// 校验不过时(返回 false = 本次不许收官):
//  1. 把「met 但缺证据」的目标退回 open——否则 goalless 分支会永远不再跑 planner,
//     任务僵住;退回后下一次唤醒重新进入 planner 路径,以证据重新证明;
//  2. 写一条 system 来源的 hint 说明原因(planner 下一轮在图上读到);
//  3. 发一条活动记录 + 唤醒 planner。
//
// 开关:settings 的 goal_evidence_gate(默认开,接线见 assembly.go);查询本身
// 出错时 fail-closed(不放行),下一轮心跳会重试。工具层(agent/goal_evidence.go)
// 管模型主动调 goal_met 的情形,这里兜住所有收官路径,两层互补。
func (e *Engine) enforceGoalEvidence(t *Task, where string) bool {
	if !agent.GoalEvidenceGateOn() || t.Store == nil {
		return true
	}
	open, unproven, err := t.Store.GoalEvidenceGap()
	if err != nil {
		log.Printf("[finalizer] task %s 证据门查询失败(%s): %v — 暂不收官", t.ID, where, err)
		return false
	}
	if len(open) == 0 && len(unproven) == 0 {
		return true
	}
	for _, g := range unproven {
		if err := t.Store.SetNodeState(g.ID, "open"); err != nil {
			log.Printf("[finalizer] task %s 目标 %d 退回 open 失败: %v", t.ID, g.ID, err)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "系统证据门(%s)：目标尚未全部以证据达成，已拒绝本次收官。", where)
	if len(open) > 0 {
		fmt.Fprintf(&b, "仍未达成：%s。", goalRefs(open))
	}
	if len(unproven) > 0 {
		fmt.Fprintf(&b, "缺少 proves 证据边、已退回 open：%s。", goalRefs(unproven))
	}
	b.WriteString("请对每个目标调 prove_goal(goal_id, evidence_id) 连上真实证据（事实/漏洞节点）；确实无法达成的目标请说明原因并调整。")
	msg := b.String()
	if _, err := t.Store.AddNode(db.KindHint, map[string]any{"text": msg}, 0, "active", "system", nil); err != nil {
		log.Printf("[finalizer] task %s 证据门 hint 写入失败: %v", t.ID, err)
	}
	e.emitActivity(t, db.Activity{Worker: "system", Kind: "text", Summary: msg})
	log.Printf("[finalizer] task %s 证据门未过(%s): open=%d unproven=%d — 拒绝收官，缺证据目标已退回并唤醒 planner", t.ID, where, len(open), len(unproven))
	t.Notify()
	return false
}

// goalRefs 渲染目标引用列表(「goal 12「文本」」),给证据门的 hint/活动文本用。
func goalRefs(ns []*db.Node) string {
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
