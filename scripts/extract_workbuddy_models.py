#!/usr/bin/env python3
"""
extract_workbuddy_models.py

Extracts model metadata, context window token limits, and max tokens from WorkBuddy's
packaged app.asar file (located in Windows AppData / WSL mount) and user configuration.

With --thinking it additionally extracts per-model thinking-level presets
(thinkingLevelMap plus OpenAI-completions compat flags) for the charon built-in
table in internal/models/thinking.go.

Usage:
    python3 scripts/extract_workbuddy_models.py [--asar /path/to/app.asar] [--format go|table|json] [--mainstream] [--thinking]
"""

import argparse
import glob
import json
import os
import re
import struct
import sys

DEFAULT_WSL_ASAR_PATTERN = "/mnt/c/Users/*/AppData/Local/Programs/WorkBuddy/resources/app.asar"
DEFAULT_USER_MODELS_PATTERN = "/mnt/c/Users/*/.workbuddy/models.json"


def find_default_asar() -> str:
    matches = glob.glob(DEFAULT_WSL_ASAR_PATTERN)
    if matches:
        return matches[0]
    return ""


def find_default_user_models() -> str:
    matches = glob.glob(DEFAULT_USER_MODELS_PATTERN)
    if matches:
        return matches[0]
    return ""


def extract_file_from_asar(asar_path: str, target_file: str) -> bytes:
    """Parses Electron ASAR archive header and extracts the raw content of target_file."""
    with open(asar_path, "rb") as f:
        magic = f.read(4)
        if magic != b"\x04\x00\x00\x00":
            raise ValueError(f"Invalid ASAR header magic: {magic!r}")
        _ = f.read(8)  # header metadata
        header_len = struct.unpack("<I", f.read(4))[0]
        header_json = f.read(header_len).decode("utf-8")
        tree = json.loads(header_json)
        base_offset = 16 + header_len

        def find_node(d, target, prefix=""):
            if "files" in d:
                for k, v in d["files"].items():
                    path = f"{prefix}/{k}" if prefix else k
                    if path == target:
                        return int(v["offset"]), int(v["size"])
                    if "files" in v:
                        res = find_node(v, target, path)
                        if res:
                            return res
            return None

        node = find_node(tree, target_file)
        if not node:
            raise FileNotFoundError(f"{target_file} not found in ASAR archive")

        offset, size = node
        f.seek(base_offset + offset)
        return f.read(size)


def parse_code_cache_models(code_cache_text: str):
    """
    Parses models registered in code-cache.js. The archive has shipped two
    encodings: escaped JSON strings (\\\"id\\\":\\\"...\\\", ... \\"contextWindow\\":1050000)
    and plain JSON ("id":"...", "contextWindow":1050000). Both carry maxTokens
    after contextWindow in each model object.
    """
    escaped = re.compile(
        r'\\\\"id\\\\":\\\\"([^\\\\"]+)\\\\",\\\\"name\\\\":\\\\"([^\\\\"]+)\\\\".*?\\\\"contextWindow\\\\":(\d+)(?:.*?\\\\"maxTokens\\\\":(\d+))?'
    )
    plain = re.compile(
        r'"id":"([^"]+)","name":"([^"]+)".*?"contextWindow":(\d+).*?"maxTokens":(\d+)'
    )
    models = {}
    for pattern in (escaped, plain):
        for m in pattern.finditer(code_cache_text):
            mid = m.group(1)
            name = m.group(2)
            cw = int(m.group(3))
            mt = int(m.group(4)) if m.group(4) else 0
            models[mid] = {"id": mid, "name": name, "contextWindow": cw, "maxTokens": mt}
    return models


def load_user_models(path: str):
    if not path or not os.path.exists(path):
        return {}
    try:
        with open(path, "r", encoding="utf-8") as f:
            content = f.read().strip()
        if not content:
            return {}
        data = json.loads(content)
        models = {}
        for item in data:
            mid = item.get("id") or item.get("name")
            if not mid:
                continue
            cw = item.get("maxInputTokens") or item.get("contextWindow") or 0
            models[mid] = {
                "id": mid,
                "name": item.get("name", mid),
                "contextWindow": int(cw),
                "maxTokens": 0,
                "custom": True,
            }
        return models
    except Exception as e:
        sys.stderr.write(f"Warning: could not read user models {path}: {e}\n")
        return {}


def family_providers(slug: str) -> tuple:
    """Official vendor channels for a model family, used to break channel ties
    so e.g. glm rows prefer z.ai over qwen-token-plan at the same tier."""
    if slug.startswith("deepseek"):
        return ("deepseek",)
    if slug.startswith("glm"):
        return ("zai", "zai-coding-cn")
    if slug.startswith("kimi"):
        return ("moonshotai", "moonshotai-cn", "kimi-coding")
    if slug.startswith(("gpt-", "o1", "o3", "o4")):
        return ("openai", "openai-codex")
    if slug.startswith("qwen"):
        return ("qwen-token-plan", "qwen-token-plan-cn", "qwen-token-plan-individual")
    return ()


def parse_thinking_models(code_cache_text: str):
    """
    Parses model objects carrying thinkingLevelMap out of code-cache.js by
    locating each occurrence, walking back to the enclosing {"id":" object,
    and brace-balancing to the end of the object.
    """
    models = {}
    for match in re.finditer(r'"thinkingLevelMap"', code_cache_text):
        start = code_cache_text.rfind('{"id":"', 0, match.start())
        if start < 0:
            continue
        depth, i, end, in_str, esc = 0, start, -1, False, False
        while i < len(code_cache_text):
            ch = code_cache_text[i]
            if in_str:
                if esc:
                    esc = False
                elif ch == "\\":
                    esc = True
                elif ch == '"':
                    in_str = False
            elif ch == '"':
                in_str = True
            elif ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
                if depth == 0:
                    end = i + 1
                    break
            i += 1
        if end < 0:
            continue
        try:
            obj = json.loads(code_cache_text[start:end])
        except json.JSONDecodeError:
            continue
        if not obj.get("thinkingLevelMap") or obj.get("api") not in ("openai-completions", "openai-responses"):
            continue
        slug = clean_slug(obj["id"]).lower()
        if not any(slug.startswith(p) or f"-{p}" in slug for p in THINKING_FAMILIES):
            continue
        compat = {k: obj["compat"][k] for k in THINKING_COMPAT_KEYS if k in (obj.get("compat") or {})}
        row = {"slug": slug, "id": obj["id"], "provider": obj.get("provider"), "api": obj.get("api"),
               "thinkingLevelMap": obj["thinkingLevelMap"], "compat": compat}
        tier = 1 if obj.get("provider") in THINKING_TIER1 else (3 if obj.get("provider") in THINKING_TIER3 else 2)
        row["tier"] = tier
        prev = models.get(slug)
        rank = (tier, 0 if obj.get("provider") in family_providers(slug) else 1, obj.get("provider") or "", obj["id"])
        if prev is None:
            models[slug] = row
        else:
            prev_rank = (prev["tier"], 0 if prev["provider"] in family_providers(slug) else 1, prev["provider"] or "", prev["id"])
            if rank < prev_rank:
                models[slug] = row
    return models


def format_thinking_levels(levels: dict) -> str:
    """Renders a thinkingLevelMap in charon's level=value TUI syntax."""
    parts = []
    for level in ("off", "minimal", "low", "medium", "high", "xhigh", "max"):
        if level not in levels:
            continue
        value = levels[level]
        parts.append(f"{level}=off" if value is None else f"{level}={value}")
    return ",".join(parts)


def is_mainstream(slug: str) -> bool:
    slug_lower = slug.lower()
    keywords = [
        "gpt-5",
        "gpt-4o",
        "o1",
        "o3",
        "claude-opus",
        "claude-sonnet",
        "claude-haiku",
        "claude-fable",
        "grok-4",
        "gemini-2.5",
        "deepseek-v4",
        "deepseek-v3",
        "kimi-k2",
        "kimi-k3",
        "glm-5",
        "glm-4.7",
        "qwen3",
    ]
    return any(k in slug_lower for k in keywords)


# thinking presets are only curated for vendors whose own endpoints speak the
# OpenAI-compatible reasoning dialect. Channel tiers: official vendor first,
# then first-party hosts, aggregators last (their maps describe their own
# routing, not the vendor API).
THINKING_FAMILIES = ("gpt-", "o1", "o3", "o4", "deepseek-", "glm-", "kimi-", "qwen3")
THINKING_TIER1 = {
    "deepseek", "zai", "zai-coding-cn", "moonshotai", "moonshotai-cn", "kimi-coding",
    "openai", "openai-codex", "qwen-token-plan", "qwen-token-plan-cn", "qwen-token-plan-individual",
}
THINKING_TIER3 = {"openrouter", "opencode", "opencode-go", "cloudflare-workers-ai"}
THINKING_COMPAT_KEYS = (
    "thinkingFormat", "supportsReasoningEffort", "maxTokensField",
    "requiresReasoningContentOnAssistantMessages", "supportsStore", "supportsDeveloperRole",
)


def clean_slug(model_id: str) -> str:
    s = model_id.strip()
    if "/" in s:
        s = s.split("/")[-1]
    for p in [
        "global.openai.",
        "openai.",
        "us.anthropic.",
        "jp.anthropic.",
        "au.anthropic.",
        "anthropic.",
        "xai.",
        "zai.",
        "moonshot.",
        "qwen.",
        "google.",
        "amazon.",
    ]:
        if s.startswith(p):
            s = s[len(p) :]
    if ":" in s:
        s = s.split(":")[0]
    return s


def main():
    parser = argparse.ArgumentParser(description="Extract WorkBuddy models and context windows")
    parser.add_argument("--asar", help="Path to WorkBuddy resources/app.asar")
    parser.add_argument("--user-models", help="Path to ~/.workbuddy/models.json")
    parser.add_argument("--format", choices=["table", "go", "json"], default="table")
    parser.add_argument("--mainstream", action="store_true", help="Filter for mainstream modern models")
    parser.add_argument("--thinking", action="store_true",
                        help="Extract thinking-level presets (thinkingLevelMap + compat) instead of context windows")
    args = parser.parse_args()

    asar_path = args.asar or find_default_asar()
    if not asar_path or not os.path.exists(asar_path):
        sys.stderr.write(f"Error: app.asar not found at {asar_path}. Pass --asar explicitly.\n")
        sys.exit(1)

    print(f"Reading ASAR: {asar_path}", file=sys.stderr)
    try:
        content_bytes = extract_file_from_asar(asar_path, "main/code-cache.js")
        code_cache_text = content_bytes.decode("utf-8", errors="ignore")
    except Exception as e:
        sys.stderr.write(f"Error extracting main/code-cache.js: {e}\n")
        sys.exit(1)

    models = parse_code_cache_models(code_cache_text)
    print(f"Extracted {len(models)} raw models from code-cache.js", file=sys.stderr)

    user_models_path = args.user_models or find_default_user_models()
    user_models = load_user_models(user_models_path)
    if user_models:
        print(f"Loaded {len(user_models)} custom models from {user_models_path}", file=sys.stderr)
        models.update(user_models)

    # Normalize by cleaned slug (lowercase for Go builtin matching)
    slug_map = {}
    for mid, info in models.items():
        slug = clean_slug(mid).lower()
        if args.mainstream and not is_mainstream(slug):
            continue
        if slug not in slug_map or info["contextWindow"] > slug_map[slug]["contextWindow"]:
            slug_map[slug] = info

    sorted_slugs = sorted(slug_map.keys())

    try:
        if args.thinking:
            thinking_models = parse_thinking_models(code_cache_text)
            print(f"Extracted {len(thinking_models)} thinking presets from code-cache.js", file=sys.stderr)
            emit_thinking(thinking_models, args)
            return
        if args.format == "json":
            out = {k: slug_map[k] for k in sorted_slugs}
            print(json.dumps(out, indent=2, ensure_ascii=False))

        elif args.format == "table":
            print(f"| {'Model Slug':<32} | {'Context Window':<14} | {'Max Tokens':<11} | {'Name':<35} |")
            print(f"|:{'-'*32}-|-{'-'*14}:|-{'-'*11}:|-{'-'*35}:|")
            for s in sorted_slugs:
                row = slug_map[s]
                cw = f"{row['contextWindow']:,}" if row['contextWindow'] > 0 else "unknown"
                mt = f"{row['maxTokens']:,}" if row['maxTokens'] > 0 else "-"
                print(f"| {s:<32} | {cw:>14} | {mt:>11} | {row['name']:<35} |")

        elif args.format == "go":
            print("// builtinModelWindows generated from WorkBuddy extract")
            print("var builtinModelWindows = []builtinRule{")
            for s in sorted_slugs:
                row = slug_map[s]
                if row["contextWindow"] <= 0:
                    continue
                mt = f', maxTokens: {row["maxTokens"]}' if row["maxTokens"] > 0 else ""
                print(f'\t{{pattern: "{s}", window: {row["contextWindow"]}{mt}}}, // {row["name"]}')
            print("}")
    except BrokenPipeError:
        devnull = os.open(os.devnull, os.O_WRONLY)
        os.dup2(devnull, sys.stdout.fileno())
        sys.exit(0)


def emit_thinking(models: dict, args):
    """Prints thinking presets as a draft for internal/models/thinking.go."""
    rows = [models[k] for k in sorted(models.keys())]
    if args.mainstream:
        rows = [r for r in rows if is_mainstream(r["slug"])]
    if args.format == "json":
        print(json.dumps(rows, indent=2, ensure_ascii=False))
        return
    if args.format == "table":
        for r in rows:
            print(f"{r['slug']:<36} {r['provider']:<22} {format_thinking_levels(r['thinkingLevelMap'])}")
            if r["compat"]:
                print(f"{'':<36} {'':<22} compat={json.dumps(r['compat'], sort_keys=True)}")
        return
    print("// draft rows generated from WorkBuddy extract;")
    print("// curate into thinkingPresets (official channels win; skip aggregator-specific flags)")
    for r in rows:
        levels = format_thinking_levels(r["thinkingLevelMap"])
        compat = r["compat"]
        extra = []
        if compat.get("thinkingFormat"):
            extra.append(f'format: "{compat["thinkingFormat"]}"')
        if "supportsReasoningEffort" in compat:
            extra.append(f"effort: boolPtr({str(compat['supportsReasoningEffort']).lower()})")
        if compat.get("maxTokensField"):
            extra.append(f'maxTokens: "{compat["maxTokensField"]}"')
        if compat.get("requiresReasoningContentOnAssistantMessages"):
            extra.append("reasoningContent: true")
        if compat.get("supportsStore") is False:
            extra.append("noStore: true")
        suffix = f", {', '.join(extra)}" if extra else ""
        print(f'\t{{pattern: "{r["slug"]}", levels: "{levels}"{suffix}}}, // {r["provider"]}')


if __name__ == "__main__":
    main()
