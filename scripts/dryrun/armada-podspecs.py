#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6"]
# ///
"""
Extract the podSpecs from an Armada JobSubmitRequest (read on stdin) and print
them as standalone v1 Pod manifests, so the pod portion of an Armada job can be
server-dry-run against a real cluster. Armada itself isn't a Kubernetes CRD, so
this is how we give Armada output Tier 3 coverage.

Usage:
    bammm convert -f armada -t armada src.yaml | armada-podspecs.py [namespace]
"""
import sys
import yaml

namespace = sys.argv[1] if len(sys.argv) > 1 else "default"
req = yaml.safe_load(sys.stdin)

docs = []
for i, job in enumerate(req.get("jobs", [])):
    pod_spec = job.get("podSpec")
    if not pod_spec:
        continue
    name = job.get("clientId") or f"armada-job-{i}"
    # Pod names must be DNS-1123 labels: lowercase alphanumerics and '-'.
    name = name.lower().replace("_", "-")[:63]
    docs.append({
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {"name": name, "namespace": namespace},
        "spec": pod_spec,
    })

if not docs:
    sys.exit("no podSpecs found in Armada request")

yaml.safe_dump_all(docs, sys.stdout, default_flow_style=False)
