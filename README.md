# OpenAI Tool Order Repair

CLIProxyAPI dynamic plugin that repairs OpenAI-compatible tool message ordering
before an upstream request is executed.

It uses the `request_interceptor` capability and fixes requests where tool
outputs appear before the assistant/function-call item they belong to.

## What it repairs

Chat Completions style:

```text
tool result -> assistant.tool_calls
```

becomes:

```text
assistant.tool_calls -> tool result
```

Responses API style:

```text
function_call_output -> function_call
```

becomes:

```text
function_call -> function_call_output
```

Unmatched tool outputs are preserved instead of being dropped.

## Online install through plugin store

Add this registry source to your CLIProxyAPI `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  store-sources:
    - "https://raw.githubusercontent.com/MEIZU16/cpa-plugin-openai-tool-order-repair/main/registry.json"
  configs:
    openai-tool-order-repair:
      enabled: true
      priority: 1
```

Restart CLIProxyAPI, then install `OpenAI Tool Order Repair` from the plugin
store UI.

## Debug logging

Version `0.1.4` writes lightweight JSONL diagnostics for the tool-order repair
decision. By default, debug records do not include request bodies, response
bodies, headers, or streaming chunks.

Example configuration:

```yaml
plugins:
  configs:
    openai-tool-order-repair:
      enabled: true
      priority: 1
      debug: true
      debug_log_path: "logs/openai-tool-order-repair-debug.jsonl"
      debug_include_body: false
      debug_log_stream_chunks: false
      debug_max_body_bytes: 4096
```

The useful default records include request size, whether a repair was applied,
message/input item counts, assistant tool-call counts, tool-result counts, and
the call IDs of tool results that were moved.

The default log path is relative to the CLIProxyAPI working directory. In the
official Docker image that usually means:

```text
/CLIProxyAPI/logs/openai-tool-order-repair-debug.jsonl
```

If `./logs` is mounted to `/CLIProxyAPI/logs`, the file will be available on the
host at:

```text
./logs/openai-tool-order-repair-debug.jsonl
```

Warning: `debug_include_body: true` may record prompts, responses, and tool
payloads. Keep it disabled unless you are investigating a specific payload.

## Management debug panel

Version `0.1.2` adds a Management API page for the debug log. After installing
or updating the plugin and restarting CLIProxyAPI, open the plugin menu entry
named `OpenAI Tool Order Repair` in the management UI.

The panel shows the current debug settings, the configured log path, the recent
tail of the JSONL log file, and a button to clear the log. It reads at most the
last 512 KiB of the log file to avoid loading very large logs into the browser.

Version `0.1.3` fixes reading debug settings from the `config_yaml` lifecycle
payload sent by CLIProxyAPI, so the panel reflects the saved `debug` switch.

Version `0.1.4` makes diagnostics lightweight by default and avoids repeating
request bodies on stream chunks.

## Build locally

```bash
go test ./...
go build -buildmode=c-shared -o openai-tool-order-repair.so .
```

## Release asset format

CLIProxyAPI expects GitHub Releases to contain:

```text
openai-tool-order-repair_<version>_<goos>_<goarch>.zip
checksums.txt
```

For Linux amd64 version `0.1.4`, the zip must contain this file at the zip root:

```text
openai-tool-order-repair.so
```

Create release assets with:

```bash
./scripts/package_release.sh 0.1.4 linux amd64
```
