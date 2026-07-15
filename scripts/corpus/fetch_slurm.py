#!/usr/bin/env python3
"""
Fetch real Slurm job scripts from GitHub into testdata/corpus/slurm/.

The script runs a set of focused search queries to get diverse coverage
(GPU, MPI, array, het-job, etc.) then fetches and quality-filters each file.
Each file is saved as <owner>__<repo>__<escaped-path> so names are unique
and traceable back to the source.

A manifest.json is written alongside the files recording the origin URL,
commit SHA, and repository star count for provenance.

Usage:
    python -m venv .venv && source .venv/bin/activate
    pip install -r scripts/corpus/requirements.txt
    export GITHUB_TOKEN=ghp_your_token_here
    python scripts/corpus/fetch_slurm.py

Options:
    --out DIR       Output directory (default: testdata/corpus/slurm)
    --limit N       Maximum files to collect (default: 300)
    --min-directives N  Minimum #SBATCH lines to accept (default: 4)
    --max-bytes N   Skip files larger than this (default: 65536)
    --token TOKEN   GitHub PAT (default: $GITHUB_TOKEN)
    --resume        Skip files already present in --out

Rate limits:
    GitHub code search: 30 requests/min with auth, 10 without.
    The script sleeps between requests; expect ~20 min for 300 files.
    Run with GITHUB_TOKEN set — unauthenticated hits the limit almost instantly.
"""

import argparse
import base64
import json
import os
import sys
import time
from pathlib import Path

import requests

# ---------------------------------------------------------------------------
# Search queries — ordered from most specific (best signal) to broadest.
# Each query targets a distinct Slurm feature so the resulting corpus covers
# the full surface area of the format, not just the most common patterns.
# ---------------------------------------------------------------------------
# Queries ordered from specific to broad.
# Each is capped at --per-query-limit files so no single query dominates.
QUERIES = [
    # Common features — bread-and-butter parser coverage
    "#SBATCH --array extension:sh",
    "#SBATCH --array extension:slurm",
    "#SBATCH --gres=gpu extension:sh",
    "#SBATCH --gres=gpu extension:slurm",
    "#SBATCH --ntasks-per-node extension:sh",
    "#SBATCH --ntasks-per-node extension:slurm",
    "#SBATCH --mem-per-cpu extension:sh",
    "#SBATCH --partition extension:slurm",
    "#SBATCH --nodes extension:sh",
    "#SBATCH --nodes extension:slurm",
    # Less-common but parser-relevant
    "#SBATCH --time-min extension:sh",         # backfill hint
    "#SBATCH --signal extension:sh",           # checkpoint / signal
    "#SBATCH --dependency extension:sh",       # dependency chains
    "#SBATCH --licenses extension:sh",         # license constraints
    "#SBATCH --bb extension:sh",               # burst buffer (NERSC/Cray)
    # Hetjob last — already well-covered in corpus
    "#SBATCH hetjob extension:sh",
    "#SBATCH hetjob extension:slurm",
]

# Only these extensions are treated as actual job scripts.
SCRIPT_EXTENSIONS = {".sh", ".slurm", ".job", ".sbatch", ".batch", ".sl", ".ll", ".bash", ""}

GITHUB_SEARCH_URL = "https://api.github.com/search/code"
GITHUB_CONTENTS_URL = "https://api.github.com/repos/{repo}/contents/{path}"


def make_session(token: str) -> requests.Session:
    s = requests.Session()
    s.headers.update({
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github.v3+json",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    return s


def search_code(session: requests.Session, query: str, per_page: int = 100):
    """Yield raw search result items, handling pagination and rate limits."""
    for page in range(1, 11):  # cap at 10 pages = 1000 results per query
        resp = _get_with_retry(session, GITHUB_SEARCH_URL, params={
            "q": query,
            "per_page": per_page,
            "page": page,
        })
        if resp is None:
            break
        data = resp.json()
        items = data.get("items", [])
        yield from items
        if len(items) < per_page:
            break
        time.sleep(1.2)  # stay under 30 search requests/min


def _get_with_retry(session, url, params=None, retries=3) -> requests.Response | None:
    for attempt in range(retries):
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
            msg = resp.json().get("message", resp.text[:120])
            print(f"    search error: {msg}", flush=True)
            return None

        if resp.status_code == 404:
            return None  # file moved / deleted

        resp.raise_for_status()

    return None


def fetch_content(session: requests.Session, item: dict) -> str | None:
    """Fetch and decode file content from the GitHub contents API."""
    url = item["url"]  # already points at contents API with ?ref=<sha>
    resp = _get_with_retry(session, url)
    if resp is None:
        return None

    data = resp.json()
    if data.get("encoding") != "base64":
        return None
    try:
        return base64.b64decode(data["content"]).decode("utf-8", errors="replace")
    except Exception:
        return None


def is_script_extension(path: str) -> bool:
    return Path(path).suffix.lower() in SCRIPT_EXTENSIONS


def quality_ok(content: str, min_directives: int, max_bytes: int) -> tuple[bool, str]:
    """Return (ok, reason). Reason is used for the --verbose skip log."""
    if len(content.encode()) > max_bytes:
        return False, f"too large ({len(content.encode())} bytes)"
    directives = [l for l in content.splitlines() if l.strip().startswith("#SBATCH")]
    if len(directives) < min_directives:
        return False, f"only {len(directives)} #SBATCH lines"
    return True, ""


def safe_filename(item: dict) -> str:
    """Build a deterministic, filesystem-safe filename from repo + path."""
    repo = item["repository"]["full_name"]  # owner/name
    path = item["path"]
    suffix = Path(path).suffix or ".sh"
    stem = (repo + "/" + path).replace("/", "__").replace(" ", "_")
    # Strip the suffix from the stem to avoid doubling it
    stem = stem.removesuffix(suffix)
    # Truncate to avoid hitting filesystem name limits
    if len(stem) > 180:
        stem = stem[:180]
    return stem + suffix


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--out", default="testdata/corpus/slurm",
                    help="Output directory (default: testdata/corpus/slurm)")
    ap.add_argument("--limit", type=int, default=300,
                    help="Max files to collect across all queries (default: 300)")
    ap.add_argument("--per-query-limit", type=int, default=25,
                    help="Max files per query, for diversity (default: 25)")
    ap.add_argument("--min-directives", type=int, default=4,
                    help="Min #SBATCH lines to accept (default: 4)")
    ap.add_argument("--max-bytes", type=int, default=65536,
                    help="Skip files larger than this (default: 65536)")
    ap.add_argument("--token", default=os.environ.get("GITHUB_TOKEN"),
                    help="GitHub PAT (default: $GITHUB_TOKEN)")
    ap.add_argument("--resume", action="store_true",
                    help="Skip files already saved in --out")
    args = ap.parse_args()

    if not args.token:
        sys.exit("Error: set GITHUB_TOKEN or pass --token.\n"
                 "Without a token you'll hit rate limits almost immediately.")

    out_dir = Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)

    # Load existing manifest if resuming
    manifest_path = out_dir / "manifest.json"
    manifest: dict = {}
    if args.resume and manifest_path.exists():
        manifest = json.loads(manifest_path.read_text())
        print(f"Resuming — {len(manifest)} files already in manifest.")

    session = make_session(args.token)
    seen_keys: set[str] = set(manifest.keys())      # deduplicate by filename
    repo_counts: dict[str, int] = {}                # max 5 files per repo
    collected = len(manifest)

    for query in QUERIES:
        if collected >= args.limit:
            break
        print(f"\nQuery: {query!r}", flush=True)
        query_collected = 0

        for item in search_code(session, query):
            if collected >= args.limit or query_collected >= args.per_query_limit:
                break

            # Skip non-script files before even fetching content
            if not is_script_extension(item["path"]):
                print(f"  SKIP (non-script ext): {item['path']}", flush=True)
                continue

            filename = safe_filename(item)
            repo = item["repository"]["full_name"]

            if filename in seen_keys:
                continue
            if repo_counts.get(repo, 0) >= 5:
                continue

            print(f"  {repo}/{item['path']} … ", end="", flush=True)

            content = fetch_content(session, item)
            if content is None:
                print("SKIP (fetch failed)")
                continue

            ok, reason = quality_ok(content, args.min_directives, args.max_bytes)
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

            seen_keys.add(filename)
            repo_counts[repo] = repo_counts.get(repo, 0) + 1
            collected += 1
            query_collected += 1
            sbatch_count = len([l for l in content.splitlines()
                                 if l.strip().startswith("#SBATCH")])
            print(f"OK ({len(content.encode())} bytes, {sbatch_count} directives)")

            time.sleep(0.4)  # polite pause between content fetches

    print(f"\nDone. {collected} files in {out_dir}/")
    print(f"Manifest: {manifest_path}")


if __name__ == "__main__":
    main()
