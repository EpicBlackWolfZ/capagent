#!/usr/bin/env python3
"""Compare exact covered statement counts, separately for each required domain."""
from collections import defaultdict
from pathlib import PurePosixPath
import sys

counts = defaultdict(lambda: [0, 0])
for line in open(sys.argv[1], encoding='utf-8'):
    if line.startswith('mode:'):
        continue
    location, statements, hits = line.split()
    package = str(PurePosixPath(location.split(':')[0]).parent)
    statements = int(statements)
    for key in (package, 'total'):
        counts[key][1] += statements
        counts[key][0] += statements if int(hits) > 0 else 0
required = {'total': False,
            'github.com/EpicBlackWolfZ/capagent/internal/model': True,
            'github.com/EpicBlackWolfZ/capagent/internal/requirement': True}
failed = False
for package, strict in required.items():
    covered, total = counts[package]
    threshold = 95.0 if strict else float(sys.argv[2])
    good = total > 0 and (covered * 100 > total * threshold if strict else covered * 100 >= total * threshold)
    percent = 100 * covered / total if total else 0
    print(f'{package}: {percent:.2f}% ({covered}/{total}), required {">" if strict else ">="}{threshold}%')
    failed |= not good
sys.exit(1 if failed else 0)
