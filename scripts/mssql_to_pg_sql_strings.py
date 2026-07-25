# -*- coding: utf-8 -*-
"""Convert MSSQL dialect to PostgreSQL ONLY inside Go raw/backtick string literals."""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(r"c:\Users\Exest\3D Objects\source")
SKIP_DIRS = {".git", "vendor", "node_modules", "dist", "canvases", "scripts"}

# Applied only inside `...` and "... long SQL ..." strings that look like SQL.
SQL_REPLACEMENTS = [
    (re.compile(r"WITH\s*\(\s*NOLOCK\s*\)", re.I), ""),
    (re.compile(r"GETUTCDATE\s*\(\s*\)", re.I), "(NOW() AT TIME ZONE 'utc')"),
    (re.compile(r"NEWID\s*\(\s*\)", re.I), "gen_random_uuid()"),
    (re.compile(r"\bISNULL\s*\(", re.I), "COALESCE("),
    (re.compile(r"\bUNIQUEIDENTIFIER\b", re.I), "UUID"),
    (re.compile(r"\bNVARCHAR\s*\(\s*MAX\s*\)", re.I), "TEXT"),
    (re.compile(r"\bNVARCHAR\s*\(\s*\d+\s*\)", re.I), "TEXT"),
    (re.compile(r"\[Order\]"), '"Order"'),
    (re.compile(r"\[([A-Za-z_][A-Za-z0-9_]*)\]"), r'"\1"'),
    (
        re.compile(
            r"OFFSET\s+(@?\w+)\s+ROWS\s+FETCH\s+NEXT\s+(@?\w+)\s+ROWS\s+ONLY",
            re.I,
        ),
        r"OFFSET \1 LIMIT \2",
    ),
    (
        re.compile(
            r"OFFSET\s+(\d+)\s+ROWS\s+FETCH\s+NEXT\s+(\d+)\s+ROWS\s+ONLY",
            re.I,
        ),
        r"OFFSET \1 LIMIT \2",
    ),
]


def convert_top_in_sql(sql: str) -> str:
    """Move SELECT TOP n to LIMIT n at end of statement (single TOP only)."""
    tops = re.findall(r"(?i)\bTOP\s+(\d+)\s+", sql)
    if not tops:
        return sql
    if len(tops) != 1:
        return re.sub(r"(?i)\bTOP\s+(\d+)\s+", r"/*TOP\1*/ ", sql)
    n = tops[0]
    new_sql = re.sub(r"(?i)\bTOP\s+\d+\s+", "", sql, count=1)
    if re.search(r"(?i)\bLIMIT\b", new_sql):
        return new_sql
    return new_sql.rstrip() + f"\nLIMIT {n}\n"


def transform_sql(sql: str) -> str:
    out = sql
    for cre, repl in SQL_REPLACEMENTS:
        out = cre.sub(repl, out)
    out = convert_top_in_sql(out)
    return out


# Match Go raw strings `...` (may contain backticks? rare) — non-greedy across newlines
RAW_STRING = re.compile(r"`([^`]*)`")
# Also double-quoted strings that contain SELECT/INSERT/UPDATE/DELETE/MERGE
DQ_SQL = re.compile(
    r'"((?:[^"\\]|\\.)*(?:SELECT|INSERT|UPDATE|DELETE|MERGE|WITH\s)(?:[^"\\]|\\.)*)"',
    re.I | re.DOTALL,
)


def process_go(text: str) -> str:
    def raw_repl(m: re.Match) -> str:
        body = m.group(1)
        if not re.search(
            r"(?i)\b(SELECT|INSERT|UPDATE|DELETE|MERGE|CREATE|ALTER|FROM|WHERE)\b",
            body,
        ):
            return m.group(0)
        return "`" + transform_sql(body) + "`"

    text = RAW_STRING.sub(raw_repl, text)

    def dq_repl(m: re.Match) -> str:
        body = m.group(1)
        # Unescape for transform then re-escape quotes minimally
        raw = body.encode("utf-8").decode("unicode_escape") if "\\" in body else body
        # Safer: transform the escaped body as-is (SQL rarely needs escapes)
        transformed = transform_sql(body)
        return '"' + transformed + '"'

    text = DQ_SQL.sub(dq_repl, text)

    # GORM struct tags (outside strings we already handle)
    text = text.replace("type:uniqueidentifier", "type:uuid")
    text = text.replace("default:NEWID()", "default:gen_random_uuid()")
    text = text.replace("default:GETUTCDATE()", "default:now()")
    return text


def main() -> int:
    changed = []
    targets = [
        ROOT / "internal" / "httpserver",
        ROOT / "internal" / "matching",
        ROOT / "internal" / "sync",
        ROOT / "internal" / "dbfimport",
        ROOT / "internal" / "models",
    ]
    for base in targets:
        for path in base.rglob("*.go"):
            orig = path.read_text(encoding="utf-8")
            new = process_go(orig)
            if new != orig:
                path.write_text(new, encoding="utf-8")
                changed.append(path.relative_to(ROOT).as_posix())
    print(f"changed {len(changed)} files")
    for c in changed:
        print(c)
    return 0


if __name__ == "__main__":
    sys.exit(main())
