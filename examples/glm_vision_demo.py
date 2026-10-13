#!/usr/bin/env python3
"""Probe GLM-5.3-Flash image input through a Sub2API gateway.

This script uses only the Python standard library. It creates a 64x64 red PNG
in memory, checks the model catalog, and sends the image through both supported
client protocols.

Examples:
    SUB2API_API_KEY=sk-xxx python3 examples/glm_vision_demo.py
    SUB2API_API_KEY=sk-xxx python3 examples/glm_vision_demo.py \
        --base-url http://localhost:8080 --model glm-5.3-flash

脚本要回答的问题：一张图片经过 Sub2API 网关转换后，到底有没有真正送到模型眼前。
做法是用一张"答案唯一"的纯红图去问模型颜色 —— 模型只要没看到图，就不可能蒙对"红"。
四种请求形态分别覆盖 Anthropic / OpenAI 两套协议里承载图片的不同位置
（普通 user 消息 vs. 工具调用结果），因为网关的协议转换层对这几条路径的处理是独立的，
任何一条漏转都会让 Claude Code 之类的客户端在"读图"环节静默失败。

判定这件事有个大坑：提示词里任何地方都不能出现答案。工具用例里的文件名早先叫
/tmp/red.png，模型直接从文件名读出 "red" 照答，图根本没送到也报 PASS。
改中性文件名后才看出真实情况。往这个脚本加用例时，务必保证除了图片本身，
上下文里没有任何能推出颜色的线索；拿不准就做对照实验——把红图换成蓝图，
如果回答不跟着变，那就是在蒙而不是在看。
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import random
import re
import struct
import sys
import urllib.error
import urllib.request
import zlib
from dataclasses import dataclass
from typing import Any, Dict, List, Optional, Tuple


# 默认打生产网关；本地联调时用 --base-url http://localhost:8080 覆盖。
DEFAULT_BASE_URL = "https://napi.metask-ai.com"
DEFAULT_MODEL = "glm-5.3-flash"
# 刻意要求"只回一个词"，方便下面用正则做机器判定，也顺带压住 token 消耗。
QUESTION = "Reply with one word only: what color is the image?"

# 每次运行随机取一种颜色，而不是固定用红色。
# 原因：被问"这是什么颜色"时模型最容易脱口而出"red"，固定红色等于把答案送给瞎猜的模型——
# 2026-09-15 就是这样连蒙带猜报出 PASS 的。随机取色后蒙中概率降到 1/len(PALETTE)，
# 且可以横向对比：同一条链路换几种颜色跑，回答不跟着变就说明模型没看见图、只是在猜。
# pattern 里带上近义词（模型可能答 navy / crimson），避免把"看对了但换个说法"判成失败。
PALETTE = [
    ("red", (230, 15, 15), r"\b(red|crimson|scarlet)\b|红"),
    ("blue", (15, 15, 230), r"\b(blue|navy|azure)\b|蓝"),
    ("green", (15, 200, 15), r"\b(green|lime)\b|绿"),
    ("yellow", (240, 240, 20), r"\b(yellow|gold)\b|黄"),
    ("purple", (140, 20, 200), r"\b(purple|violet|magenta)\b|紫"),
    ("black", (0, 0, 0), r"\bblack\b|黑"),
    ("white", (255, 255, 255), r"\bwhite\b|白"),
]


@dataclass
class HTTPResult:
    """一次 HTTP 往返的结果。

    request_bytes 记录的是请求体大小（base64 图片会把它撑到几 KB），
    出问题时可以用它判断图片是不是根本没带上，或者是不是超了上游的体积限制。
    """

    status: int
    body: Dict[str, Any]
    request_bytes: int


@dataclass
class ProbeResult:
    """单个探测用例的结论，供最后汇总打印。"""

    name: str
    protocol: str
    http_status: int
    request_bytes: int
    answer: str
    usage: Dict[str, Any]
    error: str = ""
    # 本次运行随机选中那种颜色的匹配正则，由 main 注入。
    expect: str = ""

    @property
    def passed(self) -> bool:
        r"""判定模型是否真的"看见"了这张图。

        判定条件刻意收紧：HTTP 必须 2xx、响应体里不能带 error，
        并且回答要命中本次随机颜色的 pattern。pattern 里用 \b 词边界是为了避免
        "covered"、"predicted" 这类含 red 子串的词造成误判。

        注意：单次 PASS 不足以证明图片送达了——模型蒙对的概率是 1/len(PALETTE)。
        要下结论得多跑几次看颜色是否始终跟随，或直接看 usage token 随图片尺寸变不变。
        """
        if self.http_status < 200 or self.http_status >= 300 or self.error:
            return False
        if not self.expect:
            return False
        return bool(re.search(self.expect, self.answer.strip().lower()))


def make_solid_png(width: int, height: int, rgb: Tuple[int, int, int]) -> bytes:
    """Create a small RGB PNG without Pillow or other dependencies.

    手写 PNG 是为了让脚本零依赖（生产机、容器里往往没有 Pillow）。
    PNG 的结构就是 8 字节魔数 + 若干个 (长度, 类型, 数据, CRC32) 组成的 chunk，
    最小可用集合是 IHDR（图像头）、IDAT（像素数据）、IEND（结束标记）。
    """

    # 扫描线格式：每行以 1 字节 filter type 开头（0 = None，不做行间预测），
    # 后面跟 width 个 RGB 三元组。整张图逐行拼成未压缩的原始字节流。
    raw = b"".join(b"\x00" + bytes(rgb) * width for _ in range(height))

    def chunk(kind: bytes, data: bytes) -> bytes:
        # CRC32 覆盖"类型 + 数据"，不含前面的长度字段；PNG 规定所有整数都是大端。
        checksum = zlib.crc32(kind + data) & 0xFFFFFFFF
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", checksum)

    # IHDR 字段依次是：宽、高、位深 8、颜色类型 2(truecolor RGB)、
    # 压缩方法 0、filter 方法 0、隔行扫描 0(不隔行) —— 后三项 PNG 标准只允许这些取值。
    header = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    return (
        b"\x89PNG\r\n\x1a\n"  # PNG 魔数，用于文件类型嗅探
        + chunk(b"IHDR", header)
        # IDAT 里放的是 zlib(deflate) 压缩后的扫描线；纯色图压缩率极高，成品仅几百字节。
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


class Sub2APIClient:
    """极简网关客户端：只做 JSON 收发，不引第三方 HTTP 库。"""

    def __init__(self, base_url: str, api_key: str, timeout: float) -> None:
        # 去掉结尾斜杠，后面拼 path 时才不会出现 //v1/messages。
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def request(
        self, method: str, path: str, body: Optional[Dict[str, Any]] = None
    ) -> HTTPResult:
        # separators 去掉多余空格，让 request_bytes 反映真实传输量而不是格式化开销。
        payload = b"" if body is None else json.dumps(body, separators=(",", ":")).encode()
        headers = {
            "Accept": "application/json",
            "Authorization": f"Bearer {self.api_key}",
            # 必须显式设 UA：默认的 Python-urllib/x.y 会被部分上游按 UA 直接 403，
            # 那种失败看起来很像 key 失效，极易误诊。
            "User-Agent": "sub2api-glm-vision-demo/1.0",
        }
        if body is not None:
            headers["Content-Type"] = "application/json"
        if path == "/v1/messages":
            # Anthropic 协议要求的版本头，缺了会被网关/上游直接拒。
            headers["anthropic-version"] = "2023-06-01"

        request = urllib.request.Request(
            self.base_url + path,
            data=payload if body is not None else None,
            headers=headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                raw = response.read()
                return HTTPResult(response.status, decode_json(raw), len(payload))
        except urllib.error.HTTPError as exc:
            # 4xx/5xx 不当异常抛出：错误体里往往写着真正的原因
            # （模型不存在、图片超限、上游拒绝等），要读出来给使用者看。
            raw = exc.read()
            return HTTPResult(exc.code, decode_json(raw), len(payload))


def decode_json(raw: bytes) -> Dict[str, Any]:
    """把响应体统一成 dict，保证调用方不用处理解析异常。

    网关出故障时经常返回非 JSON 的裸文本（例如负载均衡器吐的 400 页面），
    这时把前 1000 字符塞进 _raw 保留现场；顶层是 JSON 数组等非对象时塞进 _json。
    """
    try:
        value = json.loads(raw.decode("utf-8", errors="replace"))
    except json.JSONDecodeError:
        return {"_raw": raw.decode("utf-8", errors="replace")[:1000]}
    return value if isinstance(value, dict) else {"_json": value}


def error_text(body: Dict[str, Any]) -> str:
    """从各种形状的错误响应里抽出一句人能读的错误信息。

    依次兼容：OpenAI/Anthropic 风格的 {"error": {"message": ...}}、
    透传上游原文的 {"error": {"body": ...}}、error 为字符串、以及非 JSON 的裸文本。
    没有错误时返回空串——调用方用它来判断本次请求是否干净。
    """
    error = body.get("error")
    if isinstance(error, dict):
        return str(error.get("message") or error.get("body") or error)
    if error:
        return str(error)
    if "_raw" in body:
        return str(body["_raw"])
    return ""


def anthropic_answer(body: Dict[str, Any]) -> str:
    """提取 Anthropic 响应里的文本。

    content 是 block 数组，可能混有 thinking / tool_use 等类型，
    只拼接 type == "text" 的部分。所有取值都做类型防御，避免上游返回异常结构时崩掉。
    """
    content = body.get("content")
    if not isinstance(content, list):
        return ""
    return " ".join(
        str(part.get("text", ""))
        for part in content
        if isinstance(part, dict) and part.get("type") == "text"
    ).strip()


def openai_answer(body: Dict[str, Any]) -> str:
    """提取 OpenAI 响应里的文本，即 choices[0].message.content。

    逐层 isinstance 检查：模型只回工具调用时 content 会是 null，
    网关异常时 choices 也可能缺失，这些情况一律降级成空串交给 passed 判失败。
    """
    choices = body.get("choices")
    if not isinstance(choices, list) or not choices:
        return ""
    first = choices[0]
    if not isinstance(first, dict):
        return ""
    message = first.get("message")
    if not isinstance(message, dict):
        return ""
    content = message.get("content")
    return content.strip() if isinstance(content, str) else ""


def model_capability(client: Sub2APIClient, model: str) -> str:
    """查 /v1/models，看该模型有没有声明 image_input 能力。

    这一步只是"网关自称支持吗"的信息参考，不参与 PASS/FAIL 判定：
    声明与实际经常不一致（声明支持但转换层漏转、或没声明却能用），
    真正的结论以后面四个实打实的请求为准。返回值是一行可直接打印的描述。
    """
    result = client.request("GET", "/v1/models")
    if result.status < 200 or result.status >= 300:
        return f"catalog request failed: HTTP {result.status} {error_text(result.body)}"

    models = result.body.get("data")
    if not isinstance(models, list):
        return "catalog response has no data array"
    # 大小写不敏感匹配：allowlist 里同一模型可能以不同大小写重名登记。
    selected = next(
        (
            item
            for item in models
            if isinstance(item, dict)
            and str(item.get("id", "")).lower() == model.lower()
        ),
        None,
    )
    if selected is None:
        return "model is absent from catalog"
    capabilities = selected.get("capabilities")
    if not isinstance(capabilities, dict):
        return "image_input capability is not declared"
    image_input = capabilities.get("image_input")
    # 注意区分"声明了 supported=false"和"压根没声明"——后者同样按未声明报出。
    if not isinstance(image_input, dict) or "supported" not in image_input:
        return "image_input capability is not declared"
    return f"image_input.supported={image_input['supported']!r}"


def run_probe(
    client: Sub2APIClient,
    name: str,
    protocol: str,
    path: str,
    body: Dict[str, Any],
    expect: str = "",
) -> ProbeResult:
    """跑单个用例并归一成 ProbeResult；任何异常都转成失败结果，不中断其余用例。

    expect 是本次随机颜色的匹配正则，透传给 ProbeResult 做判定。
    """
    try:
        result = client.request("POST", path, body)
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        # 连不上/超时这类传输层错误没有 HTTP 状态码，用 0 占位表示"没走到响应"。
        return ProbeResult(name, protocol, 0, 0, "", {}, str(exc), expect)

    # 两套协议的响应结构不同，按 protocol 选对应的提取器。
    answer = anthropic_answer(result.body) if protocol == "Anthropic" else openai_answer(result.body)
    usage = result.body.get("usage")
    return ProbeResult(
        name=name,
        protocol=protocol,
        http_status=result.status,
        request_bytes=result.request_bytes,
        answer=answer,
        # usage 带出来是为了看图片有没有真的计入 input tokens——
        # 若 token 数和纯文本请求相当，说明图片很可能在转换途中被丢了。
        usage=usage if isinstance(usage, dict) else {},
        error=error_text(result.body),
        expect=expect,
    )


def build_cases(
    model: str, image_b64: str
) -> List[Tuple[str, str, str, Dict[str, Any]]]:
    """构造四个探测用例，返回 (用例名, 协议, 路径, 请求体) 四元组列表。

    四个用例两两成对，覆盖两条容易分头出错的代码路径：
    图片直接挂在 user 消息里，以及图片作为工具执行结果回传（Claude Code 读图就是后者）。
    """
    # 同一张图的两种协议表示：Anthropic 用结构化 source，OpenAI 用 data: URL。
    anthropic_image = {
        "type": "image",
        "source": {
            "type": "base64",
            "media_type": "image/png",
            "data": image_b64,
        },
    }
    openai_image = {
        "type": "image_url",
        "image_url": {"url": "data:image/png;base64," + image_b64},
    }
    # 工具声明本身不会被真的调用（对话里已经预置好了调用和结果），
    # 但请求里必须带上，否则带 tool_use / tool_calls 的历史消息会被判非法。
    anthropic_tools = [
        {
            "name": "Read",
            "description": "Read a file",
            "input_schema": {
                "type": "object",
                "properties": {"file_path": {"type": "string"}},
                "required": ["file_path"],
            },
        }
    ]
    openai_tools = [
        {
            "type": "function",
            "function": {
                "name": "Read",
                "description": "Read a file",
                "parameters": {
                    "type": "object",
                    "properties": {"file_path": {"type": "string"}},
                    "required": ["file_path"],
                },
            },
        }
    ]

    return [
        (
            # 用例 1：最基础的形态，图片和提问同在一条 user 消息的 content 数组里。
            "messages/user image block",
            "Anthropic",
            "/v1/messages",
            {
                "model": model,
                "max_tokens": 100,
                # 一律非流式：流式要额外解 SSE，而这里只关心最终文本。
                "stream": False,
                "messages": [
                    {
                        "role": "user",
                        "content": [
                            {"type": "text", "text": QUESTION},
                            anthropic_image,
                        ],
                    }
                ],
            },
        ),
        (
            # 用例 2：图片藏在 tool_result 里——Claude Code 用 Read 工具读图走的就是这条路径，
            # 转换层最容易在这里漏掉嵌套 content 中的 image block。
            #
            # 文件名必须保持中性（sample.png，绝不能叫 red.png）：模型会直接从文件名
            # 读出颜色照答，图没送到也能蒙对，这个用例就永远是假阳性。
            # 2026-09-15 实测踩过：文件名 red.png 配一张蓝图，模型照答 "Red"；
            # 文件名 blue.png 配红图则答 "Blue" —— 答的全是文件名。
            "messages/tool_result image block",
            "Anthropic",
            "/v1/messages",
            {
                "model": model,
                "max_tokens": 100,
                "stream": False,
                "tools": anthropic_tools,
                # 手工编造一轮完整的工具调用历史：user 提问 -> assistant 发起 tool_use
                # -> user 回传 tool_result（内容是图片）。tool_use_id 必须前后一致。
                "messages": [
                    {
                        "role": "user",
                        "content": "Read /tmp/sample.png, then answer its color with one word.",
                    },
                    {
                        "role": "assistant",
                        "content": [
                            {
                                "type": "tool_use",
                                "id": "toolu_demo_1",
                                "name": "Read",
                                "input": {"file_path": "/tmp/sample.png"},
                            }
                        ],
                    },
                    {
                        "role": "user",
                        "content": [
                            {
                                "type": "tool_result",
                                "tool_use_id": "toolu_demo_1",
                                # tool_result 的 content 本身又是 block 数组，图片嵌在这一层。
                                "content": [anthropic_image],
                            }
                        ],
                    },
                ],
            },
        ),
        (
            # 用例 3：用例 1 的 OpenAI 对应版本，图片以 data: URL 形式出现在 user 消息里。
            "chat/user image_url",
            "OpenAI",
            "/v1/chat/completions",
            {
                "model": model,
                "max_tokens": 100,
                "stream": False,
                "messages": [
                    {
                        "role": "user",
                        "content": [
                            {"type": "text", "text": QUESTION},
                            openai_image,
                        ],
                    }
                ],
            },
        ),
        (
            # 用例 4：用例 2 的 OpenAI 对应版本。注意 OpenAI 的 tool 消息只能放纯文本，
            # 所以图片没法塞进 tool 结果里，只能由 tool 消息作占位说明、
            # 紧随其后再补一条带图的 user 消息——这正是各家客户端的通行做法。
            "chat/tool result then image_url",
            "OpenAI",
            "/v1/chat/completions",
            {
                "model": model,
                "max_tokens": 100,
                "stream": False,
                "tools": openai_tools,
                "messages": [
                    {
                        "role": "user",
                        "content": "Read /tmp/sample.png, then answer its color with one word.",
                    },
                    {
                        "role": "assistant",
                        # 只发起工具调用、不说话时 content 为 null，这是 OpenAI 的约定。
                        "content": None,
                        "tool_calls": [
                            {
                                "id": "call_demo_1",
                                "type": "function",
                                "function": {
                                    "name": "Read",
                                    # OpenAI 的 arguments 是 JSON 字符串，不是对象。
                                    "arguments": '{"file_path":"/tmp/sample.png"}',
                                },
                            }
                        ],
                    },
                    {
                        "role": "tool",
                        # tool_call_id 要和上面的 tool_calls[].id 对上，否则请求非法。
                        "tool_call_id": "call_demo_1",
                        "content": "The image is attached in the next user message.",
                    },
                    {
                        "role": "user",
                        "content": [
                            {"type": "text", "text": "Image returned by Read:"},
                            openai_image,
                        ],
                    },
                ],
            },
        ),
    ]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Test glm-5.3-flash image input through Sub2API."
    )
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument("--model", default=DEFAULT_MODEL)
    # 提供 --api-key 只是兜底：命令行参数会进 shell history，也会出现在 ps 输出里。
    parser.add_argument("--api-key", help="Prefer SUB2API_API_KEY instead of this flag.")
    # 默认 120s：视觉模型首 token 慢，超时给太小会把正常响应误判成失败。
    parser.add_argument("--timeout", type=float, default=120.0)
    # 默认随机取色；排查时可以用 --color red 固定下来复现同一个请求。
    parser.add_argument(
        "--color",
        choices=[name for name, _, _ in PALETTE],
        help="Pin the probe color instead of picking one at random.",
    )
    # 同一组用例重复跑 N 轮：单轮 PASS 可能是蒙的，多轮才看得出颜色是否真的跟随图片。
    parser.add_argument("--rounds", type=int, default=1)
    parser.add_argument(
        "--save-image",
        metavar="PATH",
        help="Optionally save the generated reference PNG for inspection.",
    )
    return parser.parse_args()


def main() -> int:
    """返回进程退出码：0=四个用例全过，1=有用例未通过，2=缺 API Key。"""
    args = parse_args()
    # 取 key 的优先级：显式 --api-key > SUB2API_API_KEY > NAPI_TOKEN（老脚本沿用的变量名）。
    api_key = args.api_key or os.environ.get("SUB2API_API_KEY") or os.environ.get("NAPI_TOKEN")
    if not api_key:
        print(
            "Missing API key. Set SUB2API_API_KEY=sk-xxx or pass --api-key.",
            file=sys.stderr,
        )
        return 2

    # (220, 20, 20) 是饱和度足够高的红，避免模型答成"粉""橙"之类的邻近色。
    client = Sub2APIClient(args.base_url, api_key, args.timeout)

    # 打印运行参数便于复盘；注意 api_key 全程不落终端。
    print(f"Gateway : {client.base_url}")
    print(f"Model   : {args.model}")
    try:
        catalog = model_capability(client, args.model)
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        # 目录查询失败不影响后续探测，只记一行说明。
        catalog = f"catalog request failed: {exc}"
    print(f"Catalog : {catalog}")

    # 逐轮统计每个用例通过了几次。多轮的意义：单轮 PASS 可能是模型蒙中的，
    # 只有颜色每轮都跟着图片变，才能说明图片真的送达了。
    tally: Dict[str, int] = {}
    total = 0
    rounds = max(1, args.rounds)
    for round_index in range(rounds):
        # 每轮换一种随机颜色（--color 固定时则每轮都用它）。
        if args.color:
            color_name, rgb, pattern = next(c for c in PALETTE if c[0] == args.color)
        else:
            color_name, rgb, pattern = random.choice(PALETTE)

        png = make_solid_png(64, 64, rgb)
        if args.save_image:
            # 存盘用于人眼确认生成的 PNG 确实合法、颜色确实对得上（排查时很有用）。
            with open(args.save_image, "wb") as image_file:
                image_file.write(png)
        image_b64 = base64.b64encode(png).decode("ascii")

        print()
        print(f"--- round {round_index + 1}/{rounds}: 64x64 solid {color_name} PNG ({len(png)} bytes) ---")
        # 串行跑四个用例：彼此独立，某个失败不影响其余，最后统一汇总。
        for case_name, protocol, path, body in build_cases(args.model, image_b64):
            result = run_probe(client, case_name, protocol, path, body, pattern)
            verdict = "PASS" if result.passed else "FAIL"
            tally[case_name] = tally.get(case_name, 0) + int(result.passed)
            total += 1
            print(f"[{verdict}] {result.name}  (expected {color_name})")
            print(
                # http=0 显示成 '-'，表示请求根本没拿到响应（连不上或超时）。
                f"       protocol={result.protocol} http={result.http_status or '-'} "
                f"request_bytes={result.request_bytes}"
            )
            # 用 !r 打印原始 repr：能看出回答是空串还是带了不可见字符。
            print(f"       answer={result.answer!r}")
            if result.usage:
                print(f"       usage={json.dumps(result.usage, ensure_ascii=False)}")
            if result.error:
                print(f"       error={result.error}")

    passed = sum(tally.values())
    print()
    for case_name, hits in tally.items():
        print(f"  {hits}/{rounds}  {case_name}")
    print(f"Result: {passed}/{total} probes named the color correctly.")
    if rounds == 1:
        # 单轮只能看出"有没有明显不通"；蒙中概率是 1/len(PALETTE)，别据此下结论。
        print(
            f"Note: single round — a lucky guess has ~1/{len(PALETTE)} odds. "
            "Use --rounds 5 before trusting a PASS."
        )
    if passed == total:
        print("Multimodal image input works through all tested request shapes.")
        return 0
    # 只要有一条路径没通，就以 1 退出，方便在 CI / 验收脚本里直接卡住。
    print("At least one request shape did not deliver usable image input to the model.")
    return 1


if __name__ == "__main__":
    # 用 SystemExit 把 main 的返回值传成进程退出码。
    raise SystemExit(main())
