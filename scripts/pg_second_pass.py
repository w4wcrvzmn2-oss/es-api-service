# -*- coding: utf-8 -*-
"""Second pass: leftovers in dynamic SQL fragments."""
from pathlib import Path
import re

ROOT = Path(r"c:\Users\Exest\3D Objects\source\internal")

REPL = [
    (r"\bUNIQUEIDENTIFIER\b", "UUID"),
    (r"GETUTCDATE\s*\(\s*\)", "(NOW() AT TIME ZONE 'utc')"),
    (r"OFFSET\s+(@?\w+)\s+ROWS\s+FETCH\s+NEXT\s+(@?\w+)\s+ROWS\s+ONLY", r"OFFSET \1 LIMIT \2"),
    (r"OFFSET\s+%d\s+ROWS\s+FETCH\s+NEXT\s+%d\s+ROWS\s+ONLY", r"OFFSET %d LIMIT %d"),
    (r"OFFSET\s+(\d+)\s+ROWS\s+FETCH\s+NEXT\s+(\d+)\s+ROWS\s+ONLY", r"OFFSET \1 LIMIT \2"),
    # N'string' unicode prefix -> 'string' (SQL only; avoid Go raw words)
    (r"\bN'((?:[^']|'')*)'", r"'\1'"),
]


def main() -> None:
    changed = []
    for p in ROOT.rglob("*.go"):
        t = p.read_text(encoding="utf-8")
        nt = t
        for pat, repl in REPL:
            nt = re.sub(pat, repl, nt)
        if nt != t:
            p.write_text(nt, encoding="utf-8")
            changed.append(p)
            print(p)
    print("changed", len(changed))


if __name__ == "__main__":
    main()
