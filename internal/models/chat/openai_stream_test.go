package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterAnswerContentSuppressesInlineThinking(t *testing.T) {
	state := newStreamState()

	assert.Equal(t, "", state.filterAnswerContent("Thinking", false))
	assert.Equal(t, "", state.filterAnswerContent(" Process\nchecking law...", false))
	assert.Equal(t, "刑法的适用范围包括属地管辖。", state.filterAnswerContent("</think>\n\n刑法的适用范围包括属地管辖。", false))
	assert.Equal(t, "后续答案继续流式输出。", state.filterAnswerContent("后续答案继续流式输出。", false))
}

func TestFilterAnswerContentPassesNormalAnswer(t *testing.T) {
	state := newStreamState()

	assert.Equal(t, "刑法规定如下。", state.filterAnswerContent("刑法规定如下。", false))
	assert.Equal(t, "后续内容。", state.filterAnswerContent("后续内容。", false))
}
