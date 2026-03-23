package danmuji

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func (t *dmjTranspiler) emitAwait(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	targetNode := t.childByField(n, "target")
	durationNode := t.childByField(n, "duration")
	if nameNode == nil || targetNode == nil || durationNode == nil {
		return t.text(n)
	}

	timeout := normalizeDurationExpression(strings.TrimSpace(t.text(durationNode)), "5 * time.Second")
	channelText := t.emitAwaitChannel(targetNode)
	name := strings.TrimSpace(t.text(nameNode))

	return t.lineDirective(n) + fmt.Sprintf("%s := danmujiAwait(%s, %s, %s)", name, channelText, timeout, t.testVar)
}

func (t *dmjTranspiler) emitAwaitChannel(n *gotreesitter.Node) string {
	if n == nil {
		return ""
	}
	if t.nodeType(n) == "unary_expression" {
		if operatorNode := t.childByField(n, "operator"); operatorNode != nil && t.text(operatorNode) == "<-" {
			if operandNode := t.childByField(n, "operand"); operandNode != nil {
				return t.emit(operandNode)
			}
		}
	}
	return t.emit(n)
}
