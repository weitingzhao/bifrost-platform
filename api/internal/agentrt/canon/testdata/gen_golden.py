"""Regenerate golden.jsonl: canonical bytes as the infra recorder writes them.

The recorder's canonical form is fsutil.canonical_json in
bifrost-trade-infra/agent-config/scripts/recorder: json.dumps(obj,
sort_keys=True, separators=(",", ":"), ensure_ascii=False). Run with
python3 gen_golden.py > golden.jsonl from this directory.
"""

import hashlib
import json


def canonical_json(obj):
    return json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


CASES = [
    {},
    [],
    {"b": 1, "a": 2},
    {"z": {"y": [3, 2, 1], "x": None}, "a": True, "m": False},
    {"s": 'quote " backslash \\ slash / tab \t newline \n cr \r bs \b ff \f'},
    {"ctl": "\x00\x01\x1f\x7f"},
    {"html": "<a href='x'>&amp;</a>"},
    {"sep": "line para end"},
    {"cjk": "协作进展在正式环境", "emoji": "\U0001F680\U0001F9EA", "combining": "é"},
    {"\U0001F600": 1, "￿": 2, "a": 3, "é": 4, "B": 5},
    {"int64": [9223372036854775807, -9223372036854775808, 0, -1]},
    {"nested": [{"k": [[], {}]}, "", " "]},
    {"type": "task_proposed", "v": 1, "seq": 7, "prev_sha256": "", "body": {"title": "2.1 数据层", "needs": ["listen_ports"]}},
]

for obj in CASES:
    canon = canonical_json(obj)
    # The input is written unsorted and ASCII-escaped, so the Go side has to
    # sort keys and unescape to reach the same bytes.
    raw = json.dumps(obj, ensure_ascii=True, sort_keys=False)
    print(json.dumps({"input": raw, "canonical": canon,
                      "sha256": hashlib.sha256(canon.encode("utf-8")).hexdigest()},
                     ensure_ascii=True))
