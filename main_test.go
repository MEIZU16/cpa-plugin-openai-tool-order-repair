package main

import (
	"encoding/json"
	"testing"
)

func TestRepairOpenAIToolMessageOrderMovesToolResultAfterAssistant(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"tool","tool_call_id":"call_1","content":"tool output"},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"example","arguments":"{}"}}]}]}`)

	repaired, changed, err := repairOpenAIToolMessageOrder(body)
	if err != nil {
		t.Fatalf("repairOpenAIToolMessageOrder returned error: %v", err)
	}
	if !changed {
		t.Fatal("repairOpenAIToolMessageOrder changed = false, want true")
	}

	messages := decodeTestMessages(t, repaired)
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Role != "assistant" {
		t.Fatalf("messages[0].role = %q, want assistant", messages[0].Role)
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ID != "call_1" {
		t.Fatalf("messages[0].tool_calls = %+v, want call_1", messages[0].ToolCalls)
	}
	if messages[1].Role != "tool" || messages[1].ToolCallID != "call_1" {
		t.Fatalf("messages[1] = %+v, want tool result for call_1", messages[1])
	}
}

func TestRepairOpenAIToolMessageOrderLeavesOrderedMessagesUnchanged(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1","content":"tool output"}]}`)

	_, changed, err := repairOpenAIToolMessageOrder(body)
	if err != nil {
		t.Fatalf("repairOpenAIToolMessageOrder returned error: %v", err)
	}
	if changed {
		t.Fatal("repairOpenAIToolMessageOrder changed = true, want false")
	}
}

func TestRepairOpenAIToolMessageOrderPreservesUnmatchedToolMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","tool_call_id":"unknown","content":"unmatched"},{"role":"tool","tool_call_id":"call_1","content":"matched"},{"role":"assistant","tool_calls":[{"id":"call_1"}]}]}`)

	repaired, changed, err := repairOpenAIToolMessageOrder(body)
	if err != nil {
		t.Fatalf("repairOpenAIToolMessageOrder returned error: %v", err)
	}
	if !changed {
		t.Fatal("repairOpenAIToolMessageOrder changed = false, want true")
	}

	messages := decodeTestMessages(t, repaired)
	if len(messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(messages))
	}
	if messages[0].Role != "tool" || messages[0].ToolCallID != "unknown" {
		t.Fatalf("messages[0] = %+v, want unmatched tool preserved first", messages[0])
	}
	if messages[1].Role != "assistant" {
		t.Fatalf("messages[1].role = %q, want assistant", messages[1].Role)
	}
	if messages[2].Role != "tool" || messages[2].ToolCallID != "call_1" {
		t.Fatalf("messages[2] = %+v, want matched tool after assistant", messages[2])
	}
}

func TestRepairOpenAIToolMessageOrderRepairsResponsesInput(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"tool output"},{"type":"function_call","call_id":"call_1","name":"example","arguments":"{}"}]}`)

	repaired, changed, err := repairOpenAIToolMessageOrder(body)
	if err != nil {
		t.Fatalf("repairOpenAIToolMessageOrder returned error: %v", err)
	}
	if !changed {
		t.Fatal("repairOpenAIToolMessageOrder changed = false, want true")
	}

	input := decodeTestInput(t, repaired)
	if len(input) != 2 {
		t.Fatalf("len(input) = %d, want 2", len(input))
	}
	if input[0].Type != "function_call" || input[0].CallID != "call_1" {
		t.Fatalf("input[0] = %+v, want function_call call_1", input[0])
	}
	if input[1].Type != "function_call_output" || input[1].CallID != "call_1" {
		t.Fatalf("input[1] = %+v, want function_call_output call_1", input[1])
	}
}

func decodeTestMessages(t *testing.T, body []byte) []messageMeta {
	t.Helper()

	var root struct {
		Messages []messageMeta `json:"messages"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("decode repaired body: %v", err)
	}
	return root.Messages
}

func decodeTestInput(t *testing.T, body []byte) []messageMeta {
	t.Helper()

	var root struct {
		Input []messageMeta `json:"input"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("decode repaired body: %v", err)
	}
	return root.Input
}
