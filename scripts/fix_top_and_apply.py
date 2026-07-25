# -*- coding: utf-8 -*-
from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(r"c:\Users\Exest\3D Objects\source\internal")


def fix_top_in_sql(body: str) -> str:
    def one(mm: re.Match) -> str:
        n = mm.group(1)
        rest = mm.group(2)
        if re.search(r"\bLIMIT\b", rest, re.I):
            return "SELECT " + rest
        return "SELECT " + rest.rstrip() + f" LIMIT {n}"

    # Subquery form ending at )
    body = re.sub(
        r"SELECT\s+/\*TOP(\d+)\*/\s+((?:[^()]|\([^()]*\))*?)(?=\))",
        one,
        body,
        flags=re.I | re.S,
    )
    # Remainder to end of string
    body = re.sub(
        r"SELECT\s+/\*TOP(\d+)\*/\s+(.*)\Z",
        one,
        body,
        flags=re.I | re.S,
    )
    body = re.sub(r"/\*TOP\d+\*/\s*", "", body)
    return body


def fix_outer_apply(body: str) -> str:
    # OUTER APPLY ( ... ) alias  -> LEFT JOIN LATERAL ( ... ) alias ON true
    return re.sub(
        r"OUTER\s+APPLY\s*\(",
        "LEFT JOIN LATERAL (",
        body,
        flags=re.I,
    )


def process_file(path: Path) -> bool:
    text = path.read_text(encoding="utf-8")
    orig = text

    def raw_repl(m: re.Match) -> str:
        body = m.group(1)
        if "/*TOP" in body or re.search(r"OUTER\s+APPLY", body, re.I):
            body = fix_top_in_sql(body)
            body = fix_outer_apply(body)
            # After OUTER APPLY -> LATERAL, ensure ON true before next JOIN/WHERE if missing
            # Pattern: LATERAL ( ... ) alias\n without ON
            body = re.sub(
                r"(LEFT JOIN LATERAL\s*\((?:[^()]|\([^()]*\))*\)\s+\w+)(\s*)(?=(LEFT|INNER|RIGHT|WHERE|ORDER|GROUP|LIMIT|$))",
                r"\1 ON true\2",
                body,
                flags=re.I | re.S,
            )
        return "`" + body + "`"

    text = re.sub(r"`([^`]*)`", raw_repl, text)
    text = text.replace('\t_ "github.com/microsoft/go-mssqldb"\n', "")
    text = text.replace('\tmssqldb "github.com/microsoft/go-mssqldb"\n', "")

    if text != orig:
        path.write_text(text, encoding="utf-8")
        return True
    return False


def main() -> None:
    n = 0
    for p in ROOT.rglob("*.go"):
        if process_file(p):
            print(p)
            n += 1
    print("changed", n)


if __name__ == "__main__":
    main()
