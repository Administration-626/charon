# WorkBuddy 模型上下文长度（Context Window）提取与同步规范

本文档记录从本地 Windows 环境中安装的 WorkBuddy 客户端提取主流模型上下文长度（Context Window）元数据的寻址机制、ASAR 归档二进制解析算法与自动化同步流程。

---

## 1. 实体寻址与数据源路径

WorkBuddy 运行在宿主 Windows 系统中，在 WSL 环境中通过 `/mnt/c/` 挂载点访问：

| 实体类型 | 宿主机路径 (Windows) | WSL 挂载路径 | 作用 |
| :--- | :--- | :--- | :--- |
| **应用归档** | `%LOCALAPPDATA%\Programs\WorkBuddy\resources\app.asar` | `/mnt/c/Users/*/AppData/Local/Programs/WorkBuddy/resources/app.asar` | Electron ASAR 归档文件，内置全量模型注册表 |
| **代码缓存** | `main/code-cache.js`（归档内虚拟路径） | 需解包读取，大小约 3.5 MB | 存储包含 `contextWindow` 与 `maxTokens` 的模型配置 |
| **用户配置** | `%USERPROFILE%\.workbuddy\models.json` | `/mnt/c/Users/*/.workbuddy/models.json` | 用户在图形界面手动添加的自定义模型列表 |

---

## 2. ASAR 二进制归档解包机制

Electron 的 `.asar` 文件为二进制头接数据块的格式，无需安装第三方 Node.js `asar` 工具，可通过标准 I/O 算法解析：

1. **头部魔数校验**：读取文件前 4 字节，必须匹配 `0x04000000`（小端表示整型数值 4）。
2. **头部尺寸计算**：跳过紧接着的 8 字节元数据，读取偏移量 `12~16` 字节处的 4 字节无符号小端整型（`uint32`），获得 `header_len`。
3. **解析虚拟文件树**：从偏移量 16 字节开始读取长度为 `header_len` 的字节流，反序列化为 JSON 树形字典。
4. **定位目标节点**：在树形字典中递归搜索节点 `main/code-cache.js`，提取其字段 `offset` 与 `size`。
5. **提取原始代码**：数据段基地址为 `base_offset = 16 + header_len`。将文件指针定位至 `base_offset + offset`，连续读取 `size` 字节即得到完整的 JavaScript 文本。

---

## 3. 正则提取与数据清洗

`main/code-cache.js` 内部包含了代码缓存与内嵌 JSON 字符串。历史上 WorkBuddy 的归档曾以双引号前带反斜杠转义的形式嵌入（`\"id\":\"...`），当前版本的归档存的是未转义的普通 JSON（`"id":"..."`）；提取脚本对两种编码各持一条正则，逐条匹配：

1. **正则匹配模式**：
   ```regex
   # 转义编码（旧版归档）
   \\\"id\\\":\\\"([^\\\"]+)\\\",\\\"name\\\":\\\"([^\\\"]+)\\\".*?\\\"contextWindow\\\":(\d+)(?:.*?\\\"maxTokens\\\":(\d+))?
   # 普通 JSON 编码（当前归档）
   "id":"([^"]+)","name":"([^"]+)".*?"contextWindow":(\d+).*?"maxTokens":(\d+)
   ```
2. **Slug 归一化与剥离**：
   - 剥离斜杠命名空间（取最后一段，如 `deepseek-ai/DeepSeek-V4` -> `DeepSeek-V4`）。
   - 剥离各厂商通道前缀（如 `global.openai.`、`us.anthropic.`、`jp.anthropic.`、`xai.`、`zai.`、`moonshot.`、`qwen.`、`amazon.`）。
   - 剥离冒号后缀（如 `:batch`、`:preview`）。
   - 转换为全小写，确保与 `charon` 运行时的模型匹配逻辑一致。
3. **maxTokens 缺失处理**：普通 JSON 编码的正则把 `maxTokens` 作为必选捕获组（当前归档中每个模型对象都带该字段）；转义编码沿用可选组，未命中时记 0，同步到 Go 表时省略该字段。

---

## 4. 自动化同步脚本使用方式

工程提供独立提取工具 [`scripts/extract_workbuddy_models.py`](../scripts/extract_workbuddy_models.py)：

### 查看当前主流模型对照表
```sh
python3 scripts/extract_workbuddy_models.py --mainstream
```

### 生成 Go 语言内置切片代码（用于更新 `internal/models/builtin.go`）
```sh
python3 scripts/extract_workbuddy_models.py --mainstream --format go
```

### 导出完整 JSON 数据
```sh
python3 scripts/extract_workbuddy_models.py --format json > /tmp/workbuddy_models.json
```

---

## 5. 思考档位（thinkingLevelMap）提取与同步

WorkBuddy 的模型注册表还内置了每个模型在各自渠道上的思考档位映射
（`thinkingLevelMap`，pi 的 7 级思考级别到厂商原生 effort 值的映射，`null`
表示该级别不受支持）以及 OpenAI 兼容端点的请求格式差异（`compat`：
`thinkingFormat`、`supportsReasoningEffort`、`maxTokensField` 等）。这些数据
是 `internal/models/thinking.go` 内置思考预设表的源数据：

```sh
# 查看全部思考预设草稿（官方渠道优先，qwen/glm 等家族平级时偏好自家厂商渠道）
python3 scripts/extract_workbuddy_models.py --thinking

# 生成 Go 内置表草稿（人工策展后并入 thinkingPresets）
python3 scripts/extract_workbuddy_models.py --thinking --format go

# 完整 JSON（含渠道与 compat 明细）
python3 scripts/extract_workbuddy_models.py --thinking --format json
```

策展规则（与上下文长度同步相同的精神）：

1. **渠道优先级**：官方厂商渠道（deepseek/zai/moonshotai/openai/…）>
   一方托管（baseten/fireworks/…）> 聚合器（openrouter/opencode/…）。
   聚合器会改写 reasoning 协议，其映射只描述它自己的路由，不代表厂商 API。
2. **只收 OpenAI 兼容方言**（`openai-completions`/`openai-responses`）。
   Claude、Gemini、Grok 的官方端点不说这套方言，网关侧无权威预设，
   保持缺省（通用扩展档位）。
3. **模式匹配沿用内置上下文表的前缀规则**（`NormalizeSlug` + 前缀，
   最长模式优先），因此模式同时覆盖带日期/变体后缀的别名
   （`glm-5.3` 覆盖 `glm-5.3-flash`）。
4. **手动设置永远优先**：目录里用户通过 `set-effort` / TUI thinking map
   写入的手动映射不会被预设覆盖（`piThinkingLevelMap` 的既有语义）。
5. **方言绑定**：Claude 的官方档位表是 anthropic-messages 语义（且全部
   adaptive 模型需 `forceAdaptiveThinking` compat），只在该协议的绑定上
   生效；openai-completions 绑定下同一 slug 保持通用兑底（聚合器改写过
   的档位表不代表厂商 API）。MiniMax（官方仅 anthropic-messages 渠道）、
   Muse（无官方渠道，仅聚合器数据）、grok-4.20（早于档位 API）不收录。
   Gemini 同理。这些保持通用兑底，用户可在 TUI 手动补映射。
