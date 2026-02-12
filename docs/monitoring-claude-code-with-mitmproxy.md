# Monitoring Claude Code CLI Traffic with mitmproxy

Intercept and inspect all HTTPS traffic from Claude Code using mitmproxy — see API endpoints, headers, and streaming responses in real-time.

## Install

```bash
brew install mitmproxy
```

## Setup

### 1. Start mitmproxy (first run generates CA cert)

```bash
mitmproxy --listen-port 8080
```

### 2. Run Claude Code through the proxy

In a separate terminal:

```bash
NODE_EXTRA_CA_CERTS=~/.mitmproxy/mitmproxy-ca-cert.pem \
HTTPS_PROXY=http://localhost:8080 \
claude
```

- `NODE_EXTRA_CA_CERTS` — tells Node.js to trust mitmproxy's CA so HTTPS decryption works
- `HTTPS_PROXY` — routes all traffic through the proxy

### 3. Inspect traffic

mitmproxy provides a full TUI. You'll see every request/response including:

- API endpoints hit (e.g. `api.anthropic.com`)
- Request headers (API keys, content-type, etc.)
- Request/response bodies (including streamed SSE responses)

## Tips

- Press `?` in the mitmproxy TUI for keybindings
- Press `Enter` on a flow to see full request/response details
- Use `f` to set filters (e.g. `~d api.anthropic.com` to only show Anthropic API calls)
- Use `w` to save flows for later analysis
- `mitmweb` launches a browser-based UI instead of the TUI: `mitmweb --listen-port 8080`

## Shell alias (optional)

```bash
alias claude-proxy='NODE_EXTRA_CA_CERTS=~/.mitmproxy/mitmproxy-ca-cert.pem HTTPS_PROXY=http://localhost:8080 claude'
```

## Other approaches

| Tool | Pros | Cons |
|------|------|------|
| **Proxyman** (`brew install --cask proxyman`) | macOS-native GUI, auto CA install | GUI-only, commercial |
| **Charles Proxy** | Mature, feature-rich | GUI-only, commercial |
| **Wireshark + SSLKEYLOGFILE** | Full packet capture | Complex setup, heavy |
| **ngrep / tcpdump** | No install needed | No HTTPS decryption |

### SSLKEYLOGFILE trick (for Wireshark)

```bash
SSLKEYLOGFILE=/tmp/claude-keys.log claude
```

Then in Wireshark: Preferences > Protocols > TLS > (Pre)-Master-Secret log filename → `/tmp/claude-keys.log`
