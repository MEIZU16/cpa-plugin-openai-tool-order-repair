package main

import (
	"encoding/json"
	"strings"
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

func TestSummarizeOpenAIToolOrderReportsResponsesInputActionBeforeCall(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"tool output"},{"type":"message","role":"user","content":"between"},{"type":"function_call","call_id":"call_1","name":"example","arguments":"{}"}]}`)

	summary := summarizeOpenAIToolOrder(body)
	if summary == nil || summary.Input == nil {
		t.Fatalf("summary.Input = nil, want input repair summary")
	}
	inputSummary := summary.Input
	if !inputSummary.Changed {
		t.Fatal("inputSummary.Changed = false, want true")
	}
	if inputSummary.ToolResultCount != 1 || inputSummary.MatchedToolResultCount != 1 || inputSummary.ReorderedToolResultCount != 1 {
		t.Fatalf("input summary counts = %+v, want one matched reordered tool result", inputSummary)
	}
	if len(inputSummary.Actions) != 1 {
		t.Fatalf("len(inputSummary.Actions) = %d, want 1", len(inputSummary.Actions))
	}
	action := inputSummary.Actions[0]
	if action.CallID != "call_1" || action.FromIndex != 0 || action.ToIndex != 2 || action.Reason != "tool_result_before_function_call" {
		t.Fatalf("action = %+v, want call_1 from 0 to 2 before function call", action)
	}
}

func TestSummarizeOpenAIToolOrderReportsChatActionNotImmediatelyAfterCall(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"user","content":"between"},{"role":"tool","tool_call_id":"call_1","content":"tool output"}]}`)

	summary := summarizeOpenAIToolOrder(body)
	if summary == nil || summary.Messages == nil {
		t.Fatalf("summary.Messages = nil, want message repair summary")
	}
	messagesSummary := summary.Messages
	if !messagesSummary.Changed {
		t.Fatal("messagesSummary.Changed = false, want true")
	}
	if messagesSummary.ToolResultCount != 1 || messagesSummary.MatchedToolResultCount != 1 || messagesSummary.ReorderedToolResultCount != 1 {
		t.Fatalf("message summary counts = %+v, want one matched reordered tool result", messagesSummary)
	}
	if len(messagesSummary.Actions) != 1 {
		t.Fatalf("len(messagesSummary.Actions) = %d, want 1", len(messagesSummary.Actions))
	}
	action := messagesSummary.Actions[0]
	if action.CallID != "call_1" || action.FromIndex != 2 || action.ToIndex != 1 || action.Reason != "tool_result_not_immediately_after_function_call" {
		t.Fatalf("action = %+v, want call_1 from 2 to 1 not immediately after function call", action)
	}
}

func TestManagementPanelRegistersAndRenders(t *testing.T) {
	registrationRaw, err := handleMethod("management.register", nil)
	if err != nil {
		t.Fatalf("management.register returned error: %v", err)
	}
	var registrationEnvelope envelope
	if err := json.Unmarshal(registrationRaw, &registrationEnvelope); err != nil {
		t.Fatalf("decode management.register envelope: %v", err)
	}
	if !registrationEnvelope.OK {
		t.Fatalf("management.register ok = false, error = %+v", registrationEnvelope.Error)
	}
	var registration managementRegistration
	if err := json.Unmarshal(registrationEnvelope.Result, &registration); err != nil {
		t.Fatalf("decode management registration: %v", err)
	}
	if len(registration.Resources) != 1 || registration.Resources[0].Path != "/debug" {
		t.Fatalf("registration resources = %+v, want /debug", registration.Resources)
	}

	responseRaw, err := handleMethod("management.handle", []byte(`{"Method":"GET","Path":"/debug"}`))
	if err != nil {
		t.Fatalf("management.handle returned error: %v", err)
	}
	var responseEnvelope envelope
	if err := json.Unmarshal(responseRaw, &responseEnvelope); err != nil {
		t.Fatalf("decode management.handle envelope: %v", err)
	}
	if !responseEnvelope.OK {
		t.Fatalf("management.handle ok = false, error = %+v", responseEnvelope.Error)
	}
	var response managementResponse
	if err := json.Unmarshal(responseEnvelope.Result, &response); err != nil {
		t.Fatalf("decode management response: %v", err)
	}
	if response.StatusCode != httpStatusOK {
		t.Fatalf("StatusCode = %d, want %d", response.StatusCode, httpStatusOK)
	}
	if !strings.Contains(string(response.Body), "OpenAI Tool Order Repair") {
		t.Fatalf("management response body does not contain panel title: %s", string(response.Body))
	}
}

func TestApplyDebugConfigReadsLifecycleConfigYAML(t *testing.T) {
	previous := currentDebugSettings()
	t.Cleanup(func() {
		debugMu.Lock()
		debugConfig = previous
		debugMu.Unlock()
	})

	raw, err := json.Marshal(pluginConfigRequest{ConfigYAML: []byte(`enabled: true
priority: 0
debug: true
debug_log_path: "logs/from-yaml.jsonl"
debug_include_body: false
debug_log_stream_chunks: true
debug_max_body_bytes: 123
`)})
	if err != nil {
		t.Fatalf("marshal lifecycle config: %v", err)
	}

	applyDebugConfig(raw)
	settings := currentDebugSettings()
	if !settings.Enabled {
		t.Fatal("debug Enabled = false, want true")
	}
	if settings.LogPath != "logs/from-yaml.jsonl" {
		t.Fatalf("LogPath = %q, want logs/from-yaml.jsonl", settings.LogPath)
	}
	if settings.IncludeBody {
		t.Fatal("IncludeBody = true, want false")
	}
	if settings.LogStreamChunks {
		t.Fatal("LogStreamChunks = true, want false because stream chunk logging is disabled in lightweight diagnostics")
	}
	if !settings.StreamDiagnostics {
		t.Fatal("StreamDiagnostics = false, want true by default when debug is enabled")
	}
	if settings.MaxBodyBytes != 123 {
		t.Fatalf("MaxBodyBytes = %d, want 123", settings.MaxBodyBytes)
	}
}

func TestApplyDebugConfigStreamDiagnosticsDefaultsAndOverride(t *testing.T) {
	previous := currentDebugSettings()
	t.Cleanup(func() {
		debugMu.Lock()
		debugConfig = previous
		debugMu.Unlock()
	})

	rawDefault, err := json.Marshal(pluginConfigRequest{Config: map[string]any{
		"debug":                   true,
		"debug_log_stream_chunks": true,
	}})
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	applyDebugConfig(rawDefault)
	settings := currentDebugSettings()
	if !settings.StreamDiagnostics {
		t.Fatal("StreamDiagnostics = false, want true by default when debug is enabled")
	}
	if settings.LogStreamChunks {
		t.Fatal("LogStreamChunks = true, want false because old full stream chunk logging is ignored")
	}

	rawOverride, err := json.Marshal(pluginConfigRequest{Config: map[string]any{
		"debug":                    true,
		"debug_stream_diagnostics": false,
		"debug_log_stream_chunks":  true,
	}})
	if err != nil {
		t.Fatalf("marshal override config: %v", err)
	}
	applyDebugConfig(rawOverride)
	settings = currentDebugSettings()
	if settings.StreamDiagnostics {
		t.Fatal("StreamDiagnostics = true, want false when explicitly disabled")
	}
	if settings.LogStreamChunks {
		t.Fatal("LogStreamChunks = true, want false because old full stream chunk logging is ignored")
	}
}

func TestSummarizeStreamChunksReportsTerminalError(t *testing.T) {
	chunk := []byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"finish_reason\":\"stop\"}\n\n" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"server_error\"}}\n\n")

	summary := summarizeStreamChunks(nil, chunk, 0)
	if summary.ChunkCount != 1 || summary.HistoryChunkCount != 0 || summary.ChunkIndex != 0 {
		t.Fatalf("chunk counters = %+v, want first chunk summary", summary)
	}
	if summary.CurrentChunkBytes != len(chunk) || summary.TotalKnownBytes != len(chunk) {
		t.Fatalf("byte counters = %+v, want %d bytes", summary, len(chunk))
	}
	if summary.EventTypes["response.completed"] != 1 || summary.EventTypes["error"] != 1 {
		t.Fatalf("EventTypes = %+v, want response.completed and error counts", summary.EventTypes)
	}
	if !summary.ResponseCompleted || !summary.HasError || summary.ErrorType != "server_error" {
		t.Fatalf("terminal/error summary = %+v, want completed plus server_error", summary)
	}
	if summary.CurrentEventType != "error" || summary.TerminalEvent != "error" || summary.FinishReason != "stop" {
		t.Fatalf("event summary = %+v, want current/terminal error and finish reason stop", summary)
	}
	if !shouldLogStreamSummary(summary) {
		t.Fatal("shouldLogStreamSummary = false, want true for terminal/error summary")
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
