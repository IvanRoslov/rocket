#!/usr/bin/env python3
import argparse
import json

import pyte


def main():
    parser = argparse.ArgumentParser(description="Render a captured Codex TUI byte stream")
    parser.add_argument("path")
    parser.add_argument("--columns", type=int, default=120)
    parser.add_argument("--lines", type=int, default=40)
    args = parser.parse_args()
    screen = pyte.Screen(args.columns, args.lines)
    stream = pyte.Stream(screen)
    with open(args.path, "rb") as handle:
        stream.feed(handle.read().decode("utf-8", errors="replace"))
    print(
        json.dumps(
            {
                "cursor": {"x": screen.cursor.x, "y": screen.cursor.y},
                "lines": list(screen.display),
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
