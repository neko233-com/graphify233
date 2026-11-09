"""Generate per-file golden extractions by running the reference implementation.

The reference repo is a development-time dependency only: the produced JSON
files are committed under internal/extract/testdata and the Go tests never
need Python.

    python tools/gen_goldens.py <path-to-graphify-reference>
"""

import json
import sys
from pathlib import Path

REF = Path(sys.argv[1] if len(sys.argv) > 1 else r"D:\Code\neko233-Projects\_ref\graphify-main")
sys.path.insert(0, str(REF))

from graphify import extract  # noqa: E402

FUNCS = {
    ".py": extract.extract_python,
    ".go": extract.extract_go,
    ".ts": extract.extract_js,
    ".rs": extract.extract_rust,
    ".java": extract.extract_java,
    ".c": extract.extract_c,
    ".cpp": extract.extract_cpp,
    ".cs": extract.extract_csharp,
    ".kt": extract.extract_kotlin,
    ".scala": extract.extract_scala,
    ".php": extract.extract_php,
    ".rb": extract.extract_ruby,
}

OUT = Path(__file__).resolve().parent.parent / "internal" / "extract" / "testdata"


def main() -> None:
    for path in sorted(OUT.glob("sample*")):
        if path.suffix == ".json" or path.suffix not in FUNCS:
            continue
        fn = FUNCS[path.suffix]
        data = fn(path)
        if data.get("error"):
            print(f"{path.name}: ERROR {data['error']}")
            continue
        name = "expected_" + path.stem.replace("sample_", "") + ".json"
        if path.stem == "sample":
            name = "expected_" + path.suffix.lstrip(".") + ".json"
        (OUT / name).write_text(
            json.dumps(
                {
                    "nodes": [
                        {
                            "id": n["id"],
                            "label": n["label"],
                            "file_type": n["file_type"],
                            "source_location": n["source_location"],
                        }
                        for n in data["nodes"]
                    ],
                    "edges": [
                        {
                            "source": e["source"],
                            "target": e["target"],
                            "relation": e["relation"],
                            "confidence": e["confidence"],
                            "source_location": e["source_location"],
                            "weight": e["weight"],
                        }
                        for e in data["edges"]
                    ],
                },
                indent=1,
            )
            + "\n",
            encoding="utf-8",
        )
        print(f"{path.name}: {len(data['nodes'])} nodes, {len(data['edges'])} edges -> {name}")


if __name__ == "__main__":
    main()
