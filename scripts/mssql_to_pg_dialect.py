# -*- coding: utf-8 -*-
"""Mechanical MSSQL -> PostgreSQL dialect replacements in Go source files."""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(r"c:\Users\Exest\3D Objects\source")

SKIP_DIRS = {".git", "vendor", "node_modules", "dist", "canvases"}

# Order matters for some replacements.
REPLACEMENTS = [
    (r"WITH\s*\(\s*NOLOCK\s*\)", ""),
    (r"GETUTCDATE\s*\(\s*\)", "(NOW() AT TIME ZONE 'utc')"),
    (r"NEWID\s*\(\s*\)", "gen_random_uuid()"),
    (r"\bISNULL\s*\(", "COALESCE("),
    (r"\bUNIQUEIDENTIFIER\b", "UUID"),
    (r"\bNVARCHAR\s*\(\s*MAX\s*\)", "TEXT"),
    (r"\bNVARCHAR\s*\(\s*\d+\s*\)", "TEXT"),
    (r"\bVARCHAR\s*\(\s*MAX\s*\)", "TEXT"),
    (r"\[Order\]", '"Order"'),
    (r"\[([A-Za-z_][A-Za-z0-9_]*)\]", r'"\1"'),
    (r"OFFSET\s+(@?\w+)\s+ROWS\s+FETCH\s+NEXT\s+(@?\w+)\s+ROWS\s+ONLY", r"OFFSET \1 LIMIT \2"),
    (r"OFFSET\s+(\d+)\s+ROWS\s+FETCH\s+NEXT\s+(\d+)\s+ROWS\s+ONLY", r"OFFSET \1 LIMIT \2"),
    # SELECT TOP n → SELECT … (LIMIT added as trailing comment marker for manual pass)
    (r"(?i)\bTOP\s+(\d+)\s+", r"/*TOP\1*/ "),
]


def process_file(path: Path) -> bool:
    text = path.read_text(encoding="utf-8")
    orig = text

    if path.suffix == ".go":
        text = text.replace("type:uniqueidentifier", "type:uuid")
        text = text.replace("default:NEWID()", "default:gen_random_uuid()")
        text = text.replace("default:GETUTCDATE()", "default:now()")

    for pat, repl in REPLACEMENTS:
        text = re.sub(pat, repl, text)

    # Collapse spaces from NOLOCK removal (only multiple spaces, keep newlines)
    text = re.sub(r"[^\S\n]{2,}", " ", text)

    if text != orig:
        path.write_text(text, encoding="utf-8")
        return True
    return False


def main() -> int:
    changed = []
    for path in ROOT.rglob("*.go"):
        if any(p in SKIP_DIRS for p in path.parts):
            continue
        if process_file(path):
            changed.append(path.relative_to(ROOT).as_posix())
    print(f"changed {len(changed)} files")
    for c in changed:
        print(c)
    return 0


if __name__ == "__main__":
    sys.exit(main())
