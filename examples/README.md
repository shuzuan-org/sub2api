# GLM-5.3-Flash 多模态 Demo

`glm_vision_demo.py` 使用 Python 标准库生成一张 64×64 的纯色 PNG，并通过 Sub2API 测试：

- Anthropic `/v1/messages` 的普通 `image` block
- Anthropic `tool_result` 中的 `image` block
- OpenAI `/v1/chat/completions` 的 `image_url`
- OpenAI 工具调用后、紧随 user 消息的 `image_url`

运行：

```bash
SUB2API_API_KEY=sk-xxx python3 examples/glm_vision_demo.py --rounds 5
```

也可以指定自建网关或模型：

```bash
SUB2API_API_KEY=sk-xxx python3 examples/glm_vision_demo.py \
  --base-url http://localhost:8080 \
  --model glm-5.3-flash
```

脚本会先读取 `/v1/models` 中的 `image_input.supported`，然后输出每个请求的 HTTP 状态、请求体大小、回答和 usage。四个请求都答对当轮颜色时退出码为 `0`；否则退出码为 `1`。API Key 不会打印到终端。

## 怎么看结果（重要）

**单轮的 PASS 不能当结论。** 模型被问颜色时会直接蒙，蒙中概率约 `1/7`。判定图片是否真的送达，要三条一起看：

1. **多轮跑** —— `--rounds 5`，看回答是否每轮都跟着随机颜色变。始终跟随才算送达；时对时错就是在猜。
2. **看 usage token** —— 同一结构下把图片从 64×64 换成 512×512，input tokens 不变就说明图片没被编码进去。
3. **提示词不能泄题** —— 上下文里任何能推出颜色的线索都会造成假阳性，文件名也算（所以用 `/tmp/sample.png` 而不是 `red.png`）。

`--color red` 可以固定颜色复现单个请求，但固定颜色只适合排查，不适合做验收判定。

## 已知结论（2026-09-15，生产 napi.metask-ai.com / glm-5.3-flash）

四种请求形态**全部不通**，图片到不了模型：随机取色跑 4 轮 16 次仅命中 1 次（与纯蒙无异），且模型自己会说"Read 返回了说明文字但没有图片内容"。token 也印证——带图与不带图都是 69，换成 512×512 仍是 69。

注意 `/v1/models` 报的 `image_input.supported=true` 是假的：它来自 `backend/internal/pkg/modelsuperset/superset.go` 的硬编码兜底树（上游未上报能力时套用 Claude 的能力树），不能当验收依据。

根因尚未定位：`apicompat` 里 `anthropicUserToResponses` 等转换函数从代码上看是正确处理 image block 的，所以丢图可能发生在更下游（如 sglang-proxy 或上游本身），需要抓网关实际发往上游的请求体才能确认。
