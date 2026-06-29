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
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const abiVersion uint32 = 1

const (
	pluginID            = "openai-tool-order-repair"
	pluginVersion       = "0.1.2"
	defaultDebugLogPath = "logs/openai-tool-order-repair-debug.jsonl"
	defaultMaxBodyBytes = 256 * 1024
	debugPanelTailBytes = 512 * 1024
)

const (
	httpStatusOK                  = 200
	httpStatusNotFound            = 404
	httpStatusInternalServerError = 500
)

var debugMu sync.Mutex
var debugConfig = debugSettings{
	LogPath:         defaultDebugLogPath,
	IncludeBody:     true,
	LogStreamChunks: true,
	MaxBodyBytes:    defaultMaxBodyBytes,
}

type debugSettings struct {
	Enabled         bool
	LogPath         string
	IncludeBody     bool
	LogStreamChunks bool
	MaxBodyBytes    int
}

type pluginConfigRequest struct {
	Config map[string]any `json:"config"`
}

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
	SourceFormat   string              `json:"SourceFormat"`
	ToFormat       string              `json:"ToFormat"`
	Model          string              `json:"Model"`
	RequestedModel string              `json:"RequestedModel"`
	Stream         bool                `json:"Stream"`
	Headers        map[string][]string `json:"Headers"`
	Body           json.RawMessage     `json:"Body"`
	Metadata       map[string]any      `json:"Metadata"`
}

type interceptResponse struct {
	Body []byte `json:"Body,omitempty"`
}

type responseInterceptRequest struct {
	SourceFormat    string              `json:"SourceFormat"`
	Model           string              `json:"Model"`
	RequestedModel  string              `json:"RequestedModel"`
	Stream          bool                `json:"Stream"`
	RequestHeaders  map[string][]string `json:"RequestHeaders"`
	ResponseHeaders map[string][]string `json:"ResponseHeaders"`
	OriginalRequest json.RawMessage     `json:"OriginalRequest"`
	RequestBody     json.RawMessage     `json:"RequestBody"`
	Body            json.RawMessage     `json:"Body"`
	StatusCode      int                 `json:"StatusCode"`
	Metadata        map[string]any      `json:"Metadata"`
}

type responseInterceptResponse struct{}

type streamChunkInterceptRequest struct {
	SourceFormat    string              `json:"SourceFormat"`
	Model           string              `json:"Model"`
	RequestedModel  string              `json:"RequestedModel"`
	RequestHeaders  map[string][]string `json:"RequestHeaders"`
	ResponseHeaders map[string][]string `json:"ResponseHeaders"`
	OriginalRequest json.RawMessage     `json:"OriginalRequest"`
	RequestBody     json.RawMessage     `json:"RequestBody"`
	Body            json.RawMessage     `json:"Body"`
	HistoryChunks   []json.RawMessage   `json:"HistoryChunks"`
	ChunkIndex      int                 `json:"ChunkIndex"`
	Metadata        map[string]any      `json:"Metadata"`
}

type streamChunkInterceptResponse struct{}

type managementRegistration struct {
	Resources []resourceRoute `json:"resources,omitempty"`
}

type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementRequest struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers"`
	Query   map[string][]string `json:"Query"`
	Body    json.RawMessage     `json:"Body"`
}

type managementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body,omitempty"`
}

type debugRecord struct {
	Time              string              `json:"time"`
	Event             string              `json:"event"`
	SourceFormat      string              `json:"source_format,omitempty"`
	ToFormat          string              `json:"to_format,omitempty"`
	Model             string              `json:"model,omitempty"`
	RequestedModel    string              `json:"requested_model,omitempty"`
	Stream            bool                `json:"stream,omitempty"`
	StatusCode        int                 `json:"status_code,omitempty"`
	ChunkIndex        int                 `json:"chunk_index,omitempty"`
	HistoryChunkCount int                 `json:"history_chunk_count,omitempty"`
	Changed           bool                `json:"changed,omitempty"`
	BodyBytes         int                 `json:"body_bytes,omitempty"`
	RepairedBodyBytes int                 `json:"repaired_body_bytes,omitempty"`
	RequestHeaders    map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders   map[string][]string `json:"response_headers,omitempty"`
	Body              any                 `json:"body,omitempty"`
	RepairedBody      any                 `json:"repaired_body,omitempty"`
	OriginalRequest   any                 `json:"original_request,omitempty"`
	RequestBody       any                 `json:"request_body,omitempty"`
	Error             string              `json:"error,omitempty"`
	Metadata          map[string]any      `json:"metadata,omitempty"`
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
	case "plugin.register":
		return pluginRegistration()
	case "plugin.reconfigure":
		applyDebugConfig(rawRequest)
		return pluginRegistration()
	case "request.intercept_before", "request.intercept_after":
		return interceptOpenAIRequest(method, rawRequest)
	case "response.intercept_after":
		return interceptOpenAIResponse(rawRequest)
	case "response.intercept_stream_chunk":
		return interceptOpenAIStreamChunk(rawRequest)
	case "management.register":
		return registerManagementPanel()
	case "management.handle":
		return handleManagementRequest(rawRequest)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func pluginRegistration() ([]byte, error) {
	return okEnvelopeJSON(`{"schema_version":1,"metadata":{"Name":"openai-tool-order-repair","Version":"` + pluginVersion + `","Author":"MEIZU16","GitHubRepository":"https://github.com/MEIZU16/cpa-plugin-openai-tool-order-repair","Logo":"","ConfigFields":[{"Name":"debug","Type":"boolean","Description":"Write detailed request and response debug records."},{"Name":"debug_log_path","Type":"string","Description":"Path for JSONL debug records. Relative paths are resolved from the CLIProxyAPI working directory."},{"Name":"debug_include_body","Type":"boolean","Description":"Include request and response bodies in debug records. Sensitive data may be logged."},{"Name":"debug_log_stream_chunks","Type":"boolean","Description":"Log streaming response chunks when debug is enabled."},{"Name":"debug_max_body_bytes","Type":"integer","Description":"Maximum bytes stored for each logged body field."}]},"capabilities":{"request_interceptor":true,"response_interceptor":true,"response_stream_interceptor":true,"management_api":true}}`)
}

func registerManagementPanel() ([]byte, error) {
	return okEnvelopeValue(managementRegistration{Resources: []resourceRoute{{
		Path:        "/debug",
		Menu:        "OpenAI Tool Order Repair",
		Description: "View and clear OpenAI tool order repair debug records.",
	}}})
}

func handleManagementRequest(rawRequest []byte) ([]byte, error) {
	var req managementRequest
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return nil, fmt.Errorf("decode management request: %w", err)
	}

	switch strings.ToUpper(req.Method) {
	case "GET":
		return okEnvelopeValue(htmlManagementResponse(httpStatusOK, renderDebugPanel()))
	case "POST":
		settings := currentDebugSettings()
		if err := os.MkdirAll(filepath.Dir(settings.LogPath), 0o755); err != nil {
			return okEnvelopeValue(htmlManagementResponse(httpStatusInternalServerError, renderMessagePage("Failed to prepare debug log directory", err.Error())))
		}
		if err := os.WriteFile(settings.LogPath, nil, 0o644); err != nil {
			return okEnvelopeValue(htmlManagementResponse(httpStatusInternalServerError, renderMessagePage("Failed to clear debug log", err.Error())))
		}
		return okEnvelopeValue(htmlManagementResponse(httpStatusOK, renderMessagePage("Debug log cleared", "The debug log file has been truncated.")))
	}

	return okEnvelopeValue(htmlManagementResponse(httpStatusNotFound, renderMessagePage("Not found", "Unknown debug panel route.")))
}

func applyDebugConfig(rawRequest []byte) {
	settings := debugSettings{
		LogPath:         defaultDebugLogPath,
		IncludeBody:     true,
		LogStreamChunks: true,
		MaxBodyBytes:    defaultMaxBodyBytes,
	}

	var req pluginConfigRequest
	if len(rawRequest) > 0 {
		_ = json.Unmarshal(rawRequest, &req)
	}
	config := req.Config
	if len(config) == 0 {
		var raw map[string]any
		if err := json.Unmarshal(rawRequest, &raw); err == nil {
			config = raw
		}
	}

	settings.Enabled = boolConfig(config, "debug", false)
	settings.LogPath = stringConfig(config, "debug_log_path", defaultDebugLogPath)
	settings.IncludeBody = boolConfig(config, "debug_include_body", true)
	settings.LogStreamChunks = boolConfig(config, "debug_log_stream_chunks", true)
	settings.MaxBodyBytes = intConfig(config, "debug_max_body_bytes", defaultMaxBodyBytes)
	if settings.MaxBodyBytes <= 0 {
		settings.MaxBodyBytes = defaultMaxBodyBytes
	}

	debugMu.Lock()
	debugConfig = settings
	debugMu.Unlock()
}

func interceptOpenAIRequest(event string, rawRequest []byte) ([]byte, error) {
	var req interceptRequest
	if len(rawRequest) == 0 {
		return nil, errors.New("request payload is required")
	}
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return nil, fmt.Errorf("decode intercept request: %w", err)
	}

	body, errDecode := decodeBody(req.Body)
	if errDecode != nil {
		appendDebugRecord(debugRecord{
			Event:          event,
			SourceFormat:   req.SourceFormat,
			ToFormat:       req.ToFormat,
			Model:          req.Model,
			RequestedModel: req.RequestedModel,
			Stream:         req.Stream,
			RequestHeaders: req.Headers,
			Error:          "decode request body: " + errDecode.Error(),
			Metadata:       req.Metadata,
		})
		return nil, fmt.Errorf("decode request body: %w", errDecode)
	}
	if len(body) == 0 || !json.Valid(body) {
		appendDebugRecord(debugRecord{
			Event:          event,
			SourceFormat:   req.SourceFormat,
			ToFormat:       req.ToFormat,
			Model:          req.Model,
			RequestedModel: req.RequestedModel,
			Stream:         req.Stream,
			BodyBytes:      len(body),
			RequestHeaders: req.Headers,
			Body:           debugBodyValue(body),
			Error:          "empty or invalid JSON request body",
			Metadata:       req.Metadata,
		})
		return okEnvelopeValue(interceptResponse{})
	}

	repaired, changed, errRepair := repairOpenAIToolMessageOrder(body)
	if errRepair != nil {
		appendDebugRecord(debugRecord{
			Event:          event,
			SourceFormat:   req.SourceFormat,
			ToFormat:       req.ToFormat,
			Model:          req.Model,
			RequestedModel: req.RequestedModel,
			Stream:         req.Stream,
			BodyBytes:      len(body),
			RequestHeaders: req.Headers,
			Body:           debugBodyValue(body),
			Error:          "repair request body: " + errRepair.Error(),
			Metadata:       req.Metadata,
		})
		return nil, errRepair
	}

	appendDebugRecord(debugRecord{
		Event:             event,
		SourceFormat:      req.SourceFormat,
		ToFormat:          req.ToFormat,
		Model:             req.Model,
		RequestedModel:    req.RequestedModel,
		Stream:            req.Stream,
		Changed:           changed,
		BodyBytes:         len(body),
		RepairedBodyBytes: len(repaired),
		RequestHeaders:    req.Headers,
		Body:              debugBodyValue(body),
		RepairedBody:      debugBodyValue(repaired),
		Metadata:          req.Metadata,
	})

	if !changed {
		return okEnvelopeValue(interceptResponse{})
	}

	return okEnvelopeValue(interceptResponse{Body: repaired})
}

func interceptOpenAIResponse(rawRequest []byte) ([]byte, error) {
	var req responseInterceptRequest
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return nil, fmt.Errorf("decode response intercept request: %w", err)
	}

	body, bodyErr := decodeBody(req.Body)
	originalRequest, _ := decodeBody(req.OriginalRequest)
	requestBody, _ := decodeBody(req.RequestBody)
	record := debugRecord{
		Event:           "response.intercept_after",
		SourceFormat:    req.SourceFormat,
		Model:           req.Model,
		RequestedModel:  req.RequestedModel,
		Stream:          req.Stream,
		StatusCode:      req.StatusCode,
		BodyBytes:       len(body),
		RequestHeaders:  req.RequestHeaders,
		ResponseHeaders: req.ResponseHeaders,
		Body:            debugBodyValue(body),
		OriginalRequest: debugBodyValue(originalRequest),
		RequestBody:     debugBodyValue(requestBody),
		Metadata:        req.Metadata,
	}
	if bodyErr != nil {
		record.Error = "decode response body: " + bodyErr.Error()
	}
	appendDebugRecord(record)
	return okEnvelopeValue(responseInterceptResponse{})
}

func interceptOpenAIStreamChunk(rawRequest []byte) ([]byte, error) {
	var req streamChunkInterceptRequest
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return nil, fmt.Errorf("decode stream chunk intercept request: %w", err)
	}

	settings := currentDebugSettings()
	if settings.Enabled && !settings.LogStreamChunks && req.ChunkIndex >= 0 {
		return okEnvelopeValue(streamChunkInterceptResponse{})
	}

	body, bodyErr := decodeBody(req.Body)
	originalRequest, _ := decodeBody(req.OriginalRequest)
	requestBody, _ := decodeBody(req.RequestBody)
	record := debugRecord{
		Event:             "response.intercept_stream_chunk",
		SourceFormat:      req.SourceFormat,
		Model:             req.Model,
		RequestedModel:    req.RequestedModel,
		ChunkIndex:        req.ChunkIndex,
		HistoryChunkCount: len(req.HistoryChunks),
		BodyBytes:         len(body),
		RequestHeaders:    req.RequestHeaders,
		ResponseHeaders:   req.ResponseHeaders,
		Body:              debugBodyValue(body),
		OriginalRequest:   debugBodyValue(originalRequest),
		RequestBody:       debugBodyValue(requestBody),
		Metadata:          req.Metadata,
	}
	if bodyErr != nil {
		record.Error = "decode stream chunk body: " + bodyErr.Error()
	}
	appendDebugRecord(record)
	return okEnvelopeValue(streamChunkInterceptResponse{})
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

func boolConfig(config map[string]any, key string, fallback bool) bool {
	if config == nil {
		return fallback
	}
	v, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	case float64:
		return typed != 0
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed != 0
		}
	}
	return fallback
}

func stringConfig(config map[string]any, key string, fallback string) string {
	if config == nil {
		return fallback
	}
	v, ok := config[key]
	if !ok {
		return fallback
	}
	if typed, ok := v.(string); ok && strings.TrimSpace(typed) != "" {
		return strings.TrimSpace(typed)
	}
	return fallback
}

func intConfig(config map[string]any, key string, fallback int) int {
	if config == nil {
		return fallback
	}
	v, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := v.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func currentDebugSettings() debugSettings {
	debugMu.Lock()
	settings := debugConfig
	debugMu.Unlock()
	if settings.LogPath == "" {
		settings.LogPath = defaultDebugLogPath
	}
	if settings.MaxBodyBytes <= 0 {
		settings.MaxBodyBytes = defaultMaxBodyBytes
	}
	return settings
}

func appendDebugRecord(record debugRecord) {
	settings := currentDebugSettings()
	if !settings.Enabled {
		return
	}
	if record.Time == "" {
		record.Time = time.Now().Format(time.RFC3339Nano)
	}

	raw, err := json.Marshal(record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s debug marshal error: %v\n", pluginID, err)
		return
	}

	debugMu.Lock()
	defer debugMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(settings.LogPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "%s debug mkdir error: %v\n", pluginID, err)
		return
	}
	file, err := os.OpenFile(settings.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s debug open error: %v\n", pluginID, err)
		return
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "%s debug write error: %v\n", pluginID, err)
	}
}

func debugBodyValue(body []byte) any {
	settings := currentDebugSettings()
	if !settings.Enabled || !settings.IncludeBody || len(body) == 0 {
		return nil
	}
	if len(body) > settings.MaxBodyBytes {
		return map[string]any{
			"truncated": true,
			"bytes":     len(body),
			"prefix":    string(body[:settings.MaxBodyBytes]),
		}
	}

	var parsed any
	if json.Unmarshal(body, &parsed) == nil {
		return parsed
	}
	return string(body)
}

func htmlManagementResponse(statusCode int, body string) managementResponse {
	return managementResponse{
		StatusCode: statusCode,
		Headers: map[string][]string{
			"Content-Type": {"text/html; charset=utf-8"},
		},
		Body: []byte(body),
	}
}

func renderDebugPanel() string {
	settings := currentDebugSettings()
	logTail, totalBytes, errRead := readDebugLogTail(settings.LogPath, debugPanelTailBytes)
	status := "disabled"
	if settings.Enabled {
		status = "enabled"
	}
	streamStatus := "disabled"
	if settings.LogStreamChunks {
		streamStatus = "enabled"
	}
	bodyStatus := "disabled"
	if settings.IncludeBody {
		bodyStatus = "enabled"
	}

	logView := logTail
	if errRead != nil {
		logView = "Unable to read debug log: " + errRead.Error()
	} else if logView == "" {
		logView = "No debug records yet."
	}

	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>OpenAI Tool Order Repair Debug</title>
<style>` + managementPanelCSS() + `</style>
</head>
<body>
<main>
<h1>OpenAI Tool Order Repair</h1>
<p class="muted">Debug panel for request repair, response records, and stream chunk records.</p>
<section class="grid">
<div><strong>Version</strong><span>` + html.EscapeString(pluginVersion) + `</span></div>
<div><strong>Debug</strong><span>` + html.EscapeString(status) + `</span></div>
<div><strong>Log path</strong><span>` + html.EscapeString(settings.LogPath) + `</span></div>
<div><strong>Log bytes</strong><span>` + strconv.FormatInt(totalBytes, 10) + `</span></div>
<div><strong>Include bodies</strong><span>` + html.EscapeString(bodyStatus) + `</span></div>
<div><strong>Stream chunks</strong><span>` + html.EscapeString(streamStatus) + `</span></div>
<div><strong>Max body bytes</strong><span>` + strconv.Itoa(settings.MaxBodyBytes) + `</span></div>
<div><strong>Displayed tail</strong><span>` + strconv.Itoa(debugPanelTailBytes) + ` bytes</span></div>
</section>
<section class="actions">
<form method="get"><button type="submit">Refresh</button></form>
<form method="post" onsubmit="return confirm('Clear debug log?');"><button type="submit" class="danger">Clear log</button></form>
</section>
<section>
<h2>Recent JSONL records</h2>
<pre>` + html.EscapeString(logView) + `</pre>
</section>
</main>
</body>
</html>`
}

func renderMessagePage(title, message string) string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>` + html.EscapeString(title) + `</title>
<style>` + managementPanelCSS() + `</style>
</head>
<body>
<main>
<h1>` + html.EscapeString(title) + `</h1>
<p>` + html.EscapeString(message) + `</p>
<p><a href="debug">Back to debug panel</a></p>
</main>
</body>
</html>`
}

func managementPanelCSS() string {
	return `body{margin:0;background:#0f172a;color:#e2e8f0;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}main{max-width:1100px;margin:0 auto;padding:32px}h1{margin:0 0 8px;font-size:28px}h2{margin-top:28px}.muted{color:#94a3b8}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px;margin:24px 0}.grid div{background:#111827;border:1px solid #334155;border-radius:12px;padding:14px}.grid strong{display:block;color:#94a3b8;font-size:12px;text-transform:uppercase;letter-spacing:.04em}.grid span{display:block;margin-top:8px;overflow-wrap:anywhere}.actions{display:flex;gap:12px;margin:20px 0}button{border:0;border-radius:10px;padding:10px 16px;background:#2563eb;color:white;cursor:pointer}button.danger{background:#dc2626}pre{white-space:pre-wrap;word-break:break-word;background:#020617;border:1px solid #334155;border-radius:12px;padding:16px;max-height:70vh;overflow:auto}a{color:#93c5fd}`
}

func readDebugLogTail(path string, maxBytes int) (string, int64, error) {
	if maxBytes <= 0 {
		maxBytes = debugPanelTailBytes
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", 0, nil
		}
		return "", 0, err
	}
	if info.IsDir() {
		return "", info.Size(), fmt.Errorf("debug log path is a directory")
	}
	size := info.Size()
	if size <= int64(maxBytes) {
		raw, errRead := os.ReadFile(path)
		if errRead != nil {
			return "", size, errRead
		}
		return string(raw), size, nil
	}

	file, errOpen := os.Open(path)
	if errOpen != nil {
		return "", size, errOpen
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, maxBytes)
	start := size - int64(maxBytes)
	n, errRead := file.ReadAt(buf, start)
	if errRead != nil && n == 0 {
		return "", size, errRead
	}
	return "... truncated; showing last " + strconv.Itoa(maxBytes) + " bytes ...\n" + string(buf[:n]), size, nil
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
