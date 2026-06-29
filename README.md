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

For Linux amd64 version `0.1.0`, the zip must contain this file at the zip root:

```text
openai-tool-order-repair.so
```

Create release assets with:

```bash
./scripts/package_release.sh 0.1.0 linux amd64
```
