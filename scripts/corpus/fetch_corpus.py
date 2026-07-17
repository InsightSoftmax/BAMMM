#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["requests>=2.32.0"]
# ///
"""
Fetch real batch-job specs from GitHub into testdata/corpus/<scheduler>/.

One config-driven scraper for every scheduler BAMMM targets. Each scheduler
defines its own GitHub code-search queries, accepted file extensions, and an
"accept" predicate that confirms a fetched file really is a spec of that kind
(and is worth keeping). The generic machinery below handles pagination, rate
limits, per-repo caps, deduplication, and provenance.

A manifest.json is written alongside the files recording each file's origin
URL, commit SHA, repo star count, and the query that found it.

Usage (with uv — no manual venv needed):
    export GITHUB_TOKEN=ghp_your_token_here
    uv run scripts/corpus/fetch_corpus.py slurm
    uv run scripts/corpus/fetch_corpus.py --list
    uv run scripts/corpus/fetch_corpus.py volcano --limit 100

Options:
    --list              List supported schedulers and exit.
    --out DIR           Output dir (default: testdata/corpus/<scheduler>).
    --limit N           Max files to collect (default: 300).
    --per-query-limit N Max files per query, for diversity (default: 25).
    --per-repo-limit N  Max files per repository (default: 5).
    --min-directives N  Min directive lines for #SBATCH/#PBS schedulers (default: 4).
    --max-bytes N       Skip files larger than this (default: 65536).
    --token TOKEN       GitHub PAT (default: $GITHUB_TOKEN).
    --resume            Skip files already saved in --out.

Rate limits:
    GitHub code search allows ~30 requests/min with auth. The script paces
    itself; expect ~20 min for 300 files. Run with GITHUB_TOKEN set.
"""

import argparse
import base64
import json
import os
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

import requests

GITHUB_SEARCH_URL = "https://api.github.com/search/code"


# ── Per-scheduler accept predicates ─────────────────────────────────────────
# Each returns (ok, reason); reason is shown in the skip log when ok is False.
# `content` is the file text, `args` carries the CLI thresholds.

def _count_prefix(content: str, prefix: str) -> int:
    return sum(1 for line in content.splitlines() if line.strip().startswith(prefix))


def accept_directive(prefix: str) -> Callable:
    """Accept files with at least --min-directives lines starting with prefix."""
    def _accept(content: str, path: str, args) -> tuple[bool, str]:
        n = _count_prefix(content, prefix)
        if n < args.min_directives:
            return False, f"only {n} {prefix} lines"
        return True, ""
    return _accept


def accept_htcondor(content: str, path: str, args) -> tuple[bool, str]:
    low = content.lower()
    if "queue" not in low:
        return False, "no queue statement"
    if not any(k in low for k in ("executable", "universe", "arguments", "cmd ")):
        return False, "no executable/universe"
    return True, ""


def accept_flux(content: str, path: str, args) -> tuple[bool, str]:
    try:
        obj = json.loads(content)
    except json.JSONDecodeError:
        return False, "not JSON"
    if not isinstance(obj, dict):
        return False, "not a JSON object"
    if "resources" in obj and "tasks" in obj:
        return True, ""
    return False, "missing resources/tasks (not a Flux jobspec)"


def accept_contains(*needles: str) -> Callable:
    """Accept files containing all of the given substrings."""
    def _accept(content: str, path: str, args) -> tuple[bool, str]:
        for n in needles:
            if n not in content:
                return False, f"missing {n!r}"
        return True, ""
    return _accept


# ── Scheduler registry ──────────────────────────────────────────────────────

@dataclass
class Scheduler:
    name: str
    extensions: set[str]
    queries: list[str]
    accept: Callable
    directive: bool = False  # uses --min-directives (shown in per-file log)
    marker: str = ""         # short label for the per-file directive count log


SCHEDULERS: dict[str, Scheduler] = {
    "slurm": Scheduler(
        name="slurm",
        extensions={".sh", ".slurm", ".job", ".sbatch", ".batch", ".sl", ".bash", ""},
        directive=True, marker="#SBATCH",
        accept=accept_directive("#SBATCH"),
        queries=[
            "#SBATCH --array extension:sh",
            "#SBATCH --gres=gpu extension:sh",
            "#SBATCH --ntasks-per-node extension:sh",
            "#SBATCH --dependency extension:sh",
            "#SBATCH hetjob extension:sh",
            "#SBATCH --partition extension:slurm",
        ],
    ),
    "pbs": Scheduler(
        name="pbs",
        extensions={".sh", ".pbs", ".job", ".bash", ""},
        directive=True, marker="#PBS",
        accept=accept_directive("#PBS"),
        queries=[
            "#PBS -l select extension:sh",
            "#PBS -l nodes extension:sh",
            "#PBS -q extension:pbs",
            "#PBS -l walltime extension:sh",
            "#PBS -J extension:sh",              # array jobs
            "#PBS -W depend extension:sh",       # dependencies
        ],
    ),
    "htcondor": Scheduler(
        name="htcondor",
        extensions={".sub", ".jdl", ".condor", ".submit", ""},
        accept=accept_htcondor,
        queries=[
            "universe = vanilla queue extension:sub",
            "executable = queue extension:sub",
            "request_gpus queue extension:sub",
            "requirements queue extension:sub",
            "queue extension:jdl",
        ],
    ),
    "flux": Scheduler(
        name="flux",
        extensions={".json"},
        accept=accept_flux,
        queries=[
            '"resources" "tasks" "attributes" "version" extension:json',
            "flux jobspec extension:json",
        ],
    ),
    "volcano": Scheduler(
        name="volcano",
        extensions={".yaml", ".yml"},
        accept=accept_contains("batch.volcano.sh", "kind: Job"),
        queries=[
            "batch.volcano.sh/v1alpha1 extension:yaml",
            "schedulerName: volcano minAvailable extension:yaml",
            "batch.volcano.sh tasks extension:yaml",
        ],
    ),
    "kueue": Scheduler(
        name="kueue",
        extensions={".yaml", ".yml"},
        accept=accept_contains("kueue.x-k8s.io/queue-name"),
        queries=[
            "kueue.x-k8s.io/queue-name kind: Job extension:yaml",
            "kueue.x-k8s.io/queue-name extension:yaml",
        ],
    ),
    "armada": Scheduler(
        name="armada",
        extensions={".yaml", ".yml", ".json"},
        accept=accept_contains("jobSetId", "podSpec"),
        queries=[
            "jobSetId podSpec extension:yaml",
            "armadaproject.io jobSetId extension:yaml",
        ],
    ),
}


# ── Generic machinery ────────────────────────────────────────────────────────

def make_session(token: str) -> requests.Session:
    s = requests.Session()
    s.headers.update({
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github.v3+json",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    return s


def get_with_retry(session, url, params=None, retries=3):
    for _ in range(retries):
        resp = session.get(url, params=params)
        if resp.status_code == 200:
            return resp
        if resp.status_code in (403, 429):
            reset = int(resp.headers.get("X-RateLimit-Reset", time.time() + 60))
            wait = max(5, reset - int(time.time()) + 2)
            print(f"    rate-limited; sleeping {wait}s", flush=True)
            time.sleep(wait)
            continue
        if resp.status_code == 422:
            print(f"    search error: {resp.json().get('message', resp.text[:120])}", flush=True)
            return None
        if resp.status_code == 404:
            return None
        resp.raise_for_status()
    return None


def search_code(session, query, per_page=100):
    for page in range(1, 11):  # cap at 1000 results/query
        resp = get_with_retry(session, GITHUB_SEARCH_URL,
                               params={"q": query, "per_page": per_page, "page": page})
        if resp is None:
            break
        items = resp.json().get("items", [])
        yield from items
        if len(items) < per_page:
            break
        time.sleep(1.2)


def fetch_content(session, item):
    resp = get_with_retry(session, item["url"])
    if resp is None:
        return None
    data = resp.json()
    if data.get("encoding") != "base64":
        return None
    try:
        return base64.b64decode(data["content"]).decode("utf-8", errors="replace")
    except (ValueError, KeyError):
        return None


def safe_filename(item) -> str:
    repo = item["repository"]["full_name"]
    path = item["path"]
    suffix = Path(path).suffix or ".txt"
    stem = (repo + "/" + path).replace("/", "__").replace(" ", "_").removesuffix(suffix)
    return stem[:180] + suffix


def run(sched: Scheduler, args):
    out_dir = Path(args.out or f"testdata/corpus/{sched.name}")
    out_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = out_dir / "manifest.json"

    manifest: dict = {}
    if args.resume and manifest_path.exists():
        manifest = json.loads(manifest_path.read_text())
        print(f"Resuming — {len(manifest)} files already in manifest.")

    session = make_session(args.token)
    seen = set(manifest.keys())
    repo_counts: dict[str, int] = {}
    collected = len(manifest)

    for query in sched.queries:
        if collected >= args.limit:
            break
        print(f"\nQuery: {query!r}", flush=True)
        query_collected = 0
        for item in search_code(session, query):
            if collected >= args.limit or query_collected >= args.per_query_limit:
                break
            if Path(item["path"]).suffix.lower() not in sched.extensions:
                continue
            filename = safe_filename(item)
            repo = item["repository"]["full_name"]
            if filename in seen or repo_counts.get(repo, 0) >= args.per_repo_limit:
                continue

            print(f"  {repo}/{item['path']} … ", end="", flush=True)
            content = fetch_content(session, item)
            if content is None:
                print("SKIP (fetch failed)")
                continue
            if len(content.encode()) > args.max_bytes:
                print(f"SKIP (too large, {len(content.encode())} bytes)")
                continue
            ok, reason = sched.accept(content, item["path"], args)
            if not ok:
                print(f"SKIP ({reason})")
                continue

            (out_dir / filename).write_text(content, encoding="utf-8")
            manifest[filename] = {
                "repo": repo,
                "path": item["path"],
                "html_url": item["html_url"],
                "sha": item["sha"],
                "stars": item["repository"].get("stargazers_count", 0),
                "query": query,
            }
            manifest_path.write_text(json.dumps(manifest, indent=2))
            seen.add(filename)
            repo_counts[repo] = repo_counts.get(repo, 0) + 1
            collected += 1
            query_collected += 1
            extra = f", {_count_prefix(content, sched.marker)} {sched.marker}" if sched.directive else ""
            print(f"OK ({len(content.encode())} bytes{extra})")
            time.sleep(0.4)

    print(f"\nDone. {collected} files in {out_dir}/")


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("scheduler", nargs="?", help="scheduler to scrape (see --list)")
    ap.add_argument("--list", action="store_true", help="list supported schedulers and exit")
    ap.add_argument("--out", default="", help="output directory (default: testdata/corpus/<scheduler>)")
    ap.add_argument("--limit", type=int, default=300)
    ap.add_argument("--per-query-limit", type=int, default=25)
    ap.add_argument("--per-repo-limit", type=int, default=5)
    ap.add_argument("--min-directives", type=int, default=4)
    ap.add_argument("--max-bytes", type=int, default=65536)
    ap.add_argument("--token", default=os.environ.get("GITHUB_TOKEN"))
    ap.add_argument("--resume", action="store_true")
    args = ap.parse_args()

    if args.list or not args.scheduler:
        print("Supported schedulers:")
        for name, s in SCHEDULERS.items():
            exts = " ".join(sorted(e for e in s.extensions if e))
            print(f"  {name:10s} {exts}")
        sys.exit(0 if args.list else 2)

    sched = SCHEDULERS.get(args.scheduler)
    if sched is None:
        sys.exit(f"Unknown scheduler {args.scheduler!r}. Options: {', '.join(SCHEDULERS)}")
    if not args.token:
        sys.exit("Error: set GITHUB_TOKEN or pass --token (unauthenticated hits rate limits instantly).")

    run(sched, args)


if __name__ == "__main__":
    main()
