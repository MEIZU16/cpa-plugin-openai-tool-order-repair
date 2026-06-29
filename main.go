package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"
)

const abiVersion uint32 = 1

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type interceptRequest struct {
	SourceFormat string          `json:"SourceFormat"`
	ToFormat     string          `json:"ToFormat"`
	Body         json.RawMessage `json:"Body"`
}

type interceptResponse struct {
	Body []byte `json:"Body,omitempty"`
}

type messageMeta struct {
	Role       string         `json:"role"`
	Type       string         `json:"type"`
	CallID     string         `json:"call_id"`
	ToolCallID string         `json:"tool_call_id"`
	ToolCalls  []toolCallMeta `json:"tool_calls"`
}

type toolCallMeta struct {
	ID string `json:"id"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}

	var rawRequest []byte
	if request != nil && requestLen > 0 {
		rawRequest = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}

	raw, errHandle := handleMethod(C.GoString(method), rawRequest)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func handleMethod(method string, rawRequest []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return okEnvelopeJSON(`{"schema_version":1,"metadata":{"Name":"openai-tool-order-repair","Version":"0.1.0","Author":"MEIZU16","GitHubRepository":"https://github.com/MEIZU16/cpa-plugin-openai-tool-order-repair","Logo":"","ConfigFields":[]},"capabilities":{"request_interceptor":true}}`)
	case "request.intercept_before", "request.intercept_after":
		return interceptOpenAIRequest(rawRequest)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func interceptOpenAIRequest(rawRequest []byte) ([]byte, error) {
	var req interceptRequest
	if len(rawRequest) == 0 {
		return nil, errors.New("request payload is required")
	}
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return nil, fmt.Errorf("decode intercept request: %w", err)
	}

	body, errDecode := decodeBody(req.Body)
	if errDecode != nil {
		return nil, fmt.Errorf("decode request body: %w", errDecode)
	}
	if len(body) == 0 || !json.Valid(body) {
		return okEnvelopeValue(interceptResponse{})
	}

	repaired, changed, errRepair := repairOpenAIToolMessageOrder(body)
	if errRepair != nil {
		return nil, errRepair
	}
	if !changed {
		return okEnvelopeValue(interceptResponse{})
	}

	return okEnvelopeValue(interceptResponse{Body: repaired})
}

func decodeBody(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var body []byte
	if err := json.Unmarshal(raw, &body); err == nil {
		return body, nil
	}

	var bodyString string
	if err := json.Unmarshal(raw, &bodyString); err != nil {
		return nil, err
	}
	return []byte(bodyString), nil
}

func repairOpenAIToolMessageOrder(body []byte) ([]byte, bool, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false, fmt.Errorf("decode OpenAI request body: %w", err)
	}

	changed := false
	if rawMessages, okMessages := root["messages"]; okMessages {
		reorderedMessages, messagesChanged, errMessages := reorderOpenAIItems(rawMessages)
		if errMessages != nil {
			return nil, false, fmt.Errorf("repair messages: %w", errMessages)
		}
		if messagesChanged {
			root["messages"] = reorderedMessages
			changed = true
		}
	}

	if rawInput, okInput := root["input"]; okInput {
		reorderedInput, inputChanged, errInput := reorderOpenAIItems(rawInput)
		if errInput != nil {
			return nil, false, fmt.Errorf("repair input: %w", errInput)
		}
		if inputChanged {
			root["input"] = reorderedInput
			changed = true
		}
	}

	if !changed {
		return body, false, nil
	}

	repaired, errRepair := json.Marshal(root)
	if errRepair != nil {
		return nil, false, fmt.Errorf("encode repaired request body: %w", errRepair)
	}
	return repaired, true, nil
}

func reorderOpenAIItems(rawItems json.RawMessage) (json.RawMessage, bool, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(rawItems, &items); err != nil {
		return rawItems, false, nil
	}

	reordered, changed := reorderMessages(items)
	if !changed {
		return rawItems, false, nil
	}

	rawReordered, err := json.Marshal(reordered)
	if err != nil {
		return nil, false, fmt.Errorf("encode reordered items: %w", err)
	}
	return rawReordered, true, nil
}

func reorderMessages(messages []json.RawMessage) ([]json.RawMessage, bool) {
	metas := make([]messageMeta, len(messages))
	toolResultIndexesByID := make(map[string][]int)
	assistantToolIDs := make(map[string]struct{})

	for i, raw := range messages {
		_ = json.Unmarshal(raw, &metas[i])
		if toolResultID := getToolResultID(metas[i]); toolResultID != "" {
			toolResultIndexesByID[toolResultID] = append(toolResultIndexesByID[toolResultID], i)
		}
		for _, toolCallID := range getAssistantToolCallIDs(metas[i]) {
			if toolCallID != "" {
				assistantToolIDs[toolCallID] = struct{}{}
			}
		}
	}

	if len(toolResultIndexesByID) == 0 || len(assistantToolIDs) == 0 {
		return messages, false
	}

	inserted := make([]bool, len(messages))
	reordered := make([]json.RawMessage, 0, len(messages))
	for i, raw := range messages {
		meta := metas[i]
		if toolResultID := getToolResultID(meta); toolResultID != "" {
			if _, hasAssistant := assistantToolIDs[toolResultID]; hasAssistant {
				continue
			}
		}

		reordered = append(reordered, raw)
		toolCallIDs := getAssistantToolCallIDs(meta)
		if len(toolCallIDs) == 0 {
			continue
		}

		for _, toolCallID := range toolCallIDs {
			if toolCallID == "" {
				continue
			}
			for _, resultIndex := range toolResultIndexesByID[toolCallID] {
				if inserted[resultIndex] {
					continue
				}
				reordered = append(reordered, messages[resultIndex])
				inserted[resultIndex] = true
			}
		}
	}

	if len(reordered) != len(messages) {
		return messages, false
	}
	return reordered, !sameMessageOrder(messages, reordered)
}

func sameMessageOrder(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(bytes.TrimSpace(a[i]), bytes.TrimSpace(b[i])) {
			return false
		}
	}
	return true
}

func getToolResultID(meta messageMeta) string {
	if meta.Role == "tool" && meta.ToolCallID != "" {
		return meta.ToolCallID
	}
	if meta.Type == "function_call_output" && meta.CallID != "" {
		return meta.CallID
	}
	return ""
}

func getAssistantToolCallIDs(meta messageMeta) []string {
	if meta.Type == "function_call" && meta.CallID != "" {
		return []string{meta.CallID}
	}
	if meta.Role != "assistant" || len(meta.ToolCalls) == 0 {
		return nil
	}

	ids := make([]string, 0, len(meta.ToolCalls))
	for _, toolCall := range meta.ToolCalls {
		if toolCall.ID != "" {
			ids = append(ids, toolCall.ID)
		}
	}
	return ids
}

func okEnvelopeJSON(result string) ([]byte, error) {
	return json.Marshal(envelope{OK: true, Result: json.RawMessage(result)})
}

func okEnvelopeValue(result any) ([]byte, error) {
	rawResult, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: rawResult})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
