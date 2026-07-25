# -*- coding: utf-8 -*-
from pathlib import Path

ROOT = Path(r"c:\Users\Exest\3D Objects\source\internal")


def fix_file(path: Path) -> bool:
    t = path.read_text(encoding="utf-8")
    orig = t
    t = t.replace('fmt.Errorf(""Order" insert: %w"', 'fmt.Errorf("Order insert: %w"')
    t = t.replace('fmt.Errorf(""Order" update: %w"', 'fmt.Errorf("Order update: %w"')

    # Fix invalid Go string literals that contain raw newlines
    out = []
    i = 0
    n = len(t)
    while i < n:
        ch = t[i]
        if ch == "`":
            j = t.find("`", i + 1)
            if j < 0:
                out.append(t[i:])
                break
            out.append(t[i : j + 1])
            i = j + 1
            continue
        if ch == '"':
            j = i + 1
            broken = False
            while j < n:
                if t[j] == "\n":
                    broken = True
                    j += 1
                    continue
                if t[j] == "\\" and j + 1 < n:
                    j += 2
                    continue
                if t[j] == '"':
                    break
                j += 1
            if broken and j < n and t[j] == '"':
                inner = t[i + 1 : j].replace("\r", "").replace("\n", r"\n")
                out.append('"' + inner + '"')
                i = j + 1
                continue
            if j < n:
                out.append(t[i : j + 1])
                i = j + 1
                continue
        out.append(ch)
        i += 1

    t = "".join(out)
    if t != orig:
        path.write_text(t, encoding="utf-8")
        return True
    return False


def main() -> None:
    for p in ROOT.rglob("*.go"):
        if fix_file(p):
            print("fixed", p)


if __name__ == "__main__":
    main()
