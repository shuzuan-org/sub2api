#!/usr/bin/env python3
"""napi.metask-ai.com 多模态透传探针(标准库,无依赖)。

用法:
    NAPI_TOKEN=<api_key> python3 napi_vision_probe.py [model] [gateway]
默认 model=glm-5.3-flash, gateway=https://napi.metask-ai.com

做什么:生成一张 64x64 纯红 PNG,分别用四种放法发给网关,打印模型回答。
预期:四种都应回 "Red"(或红色)。实测 2026-09-11:四种全部看不到图,而 /v1/models 对该模型
声明 capabilities.image_input.supported=true。
"""
import base64, json, os, struct, sys, urllib.request, zlib

TOKEN = os.environ.get("NAPI_TOKEN") or sys.exit("set NAPI_TOKEN=<api_key>")
MODEL = sys.argv[1] if len(sys.argv) > 1 else "glm-5.3-flash"
GATEWAY = (sys.argv[2] if len(sys.argv) > 2 else "https://napi.metask-ai.com").rstrip("/")


def png_rgb(w, h, rgb):
    raw = b"".join(b"\x00" + bytes(rgb) * w for _ in range(h))
    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


B64 = base64.b64encode(png_rgb(64, 64, (220, 20, 20))).decode()
Q = "Reply with one word only: what colour is the image?"


def post(path, body):
    req = urllib.request.Request(GATEWAY + path, data=json.dumps(body).encode(),
                                 headers={"Authorization": "Bearer " + TOKEN, "content-type": "application/json",
                                          "anthropic-version": "2023-06-01"})
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return {"error": {"status": e.code, "body": e.read().decode()[:300]}}


def anth_text(d):
    return " ".join(c.get("text", "") for c in d.get("content", []) if c.get("type") == "text").strip()


def oai_text(d):
    ch = (d.get("choices") or [{}])[0]
    return (ch.get("message", {}).get("content") or "").strip(), ch.get("finish_reason")


img_a = {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": B64}}
img_o = {"type": "image_url", "image_url": {"url": "data:image/png;base64," + B64}}
read_tool_a = [{"name": "Read", "description": "Reads a file",
                "input_schema": {"type": "object", "properties": {"file_path": {"type": "string"}}, "required": ["file_path"]}}]
read_tool_o = [{"type": "function", "function": {"name": "Read", "description": "Reads a file",
                "parameters": {"type": "object", "properties": {"file_path": {"type": "string"}}, "required": ["file_path"]}}}]

cases = []
# A: Anthropic Messages, image block in the user message
A = post("/v1/messages", {"model": MODEL, "max_tokens": 300,
                          "messages": [{"role": "user", "content": [{"type": "text", "text": Q}, img_a]}]})
cases.append(("A /v1/messages  user-message image block", A.get("stop_reason"), anth_text(A), A.get("error")))
# B: Anthropic Messages, image block inside tool_result (what an agent's Read tool sends)
B = post("/v1/messages", {"model": MODEL, "max_tokens": 300, "tools": read_tool_a, "messages": [
    {"role": "user", "content": "Read /tmp/box.png then reply with one word only: what colour is it?"},
    {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "/tmp/box.png"}}]},
    {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_01", "content": [img_a]}]}]})
cases.append(("B /v1/messages  tool_result image block", B.get("stop_reason"), anth_text(B), B.get("error")))
# D: OpenAI chat/completions, image_url in the user message
D = post("/v1/chat/completions", {"model": MODEL, "max_tokens": 300,
                                  "messages": [{"role": "user", "content": [{"type": "text", "text": Q}, img_o]}]})
dt, df = oai_text(D)
cases.append(("D /v1/chat/completions  user image_url", df, dt, D.get("error")))
# E: OpenAI chat/completions, tool message text + following user message carrying the image_url
E = post("/v1/chat/completions", {"model": MODEL, "max_tokens": 300, "tools": read_tool_o, "messages": [
    {"role": "user", "content": "Read /tmp/box.png then reply with one word only: what colour is it?"},
    {"role": "assistant", "content": None, "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "Read", "arguments": "{\"file_path\":\"/tmp/box.png\"}"}}]},
    {"role": "tool", "tool_call_id": "call_1", "content": "[image attached in the next message]"},
    {"role": "user", "content": [{"type": "text", "text": "Image from Read:"}, img_o]}]})
et, ef = oai_text(E)
cases.append(("E /v1/chat/completions  tool text + follow-up user image_url", ef, et, E.get("error")))

print(f"model={MODEL} gateway={GATEWAY} image=64x64 solid red PNG ({len(B64)} b64 chars)")
for name, stop, text, err in cases:
    print(f"{name:<62} stop={stop!s:<10} answer={text[:80]!r}" + (f" error={err}" if err else ""))
