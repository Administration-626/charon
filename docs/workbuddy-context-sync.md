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

`main/code-cache.js` 内部包含了代码缓存与内嵌 JSON 字符串，其中的双引号带有反斜杠转义：

1. **正则匹配模式**：
   ```regex
   \\\"id\\\":\\\"([^\\\"]+)\\\",\\\"name\\\":\\\"([^\\\"]+)\\\".*?\\\"contextWindow\\\":(\d+)(?:.*?\\\"maxTokens\\\":(\d+))?
   ```
2. **Slug 归一化与剥离**：
   - 剥离斜杠命名空间（取最后一段，如 `deepseek-ai/DeepSeek-V4` -> `DeepSeek-V4`）。
   - 剥离各厂商通道前缀（如 `global.openai.`、`us.anthropic.`、`jp.anthropic.`、`xai.`、`zai.`、`moonshot.`、`qwen.`、`amazon.`）。
   - 剥离冒号后缀（如 `:batch`、`:preview`）。
   - 转换为全小写，确保与 `charon` 运行时的模型匹配逻辑一致。

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
