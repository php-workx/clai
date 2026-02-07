#!/usr/bin/env python3
"""Fetch SonarQube issues and keep only issues on changed files at/above a severity threshold."""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter
from typing import Any, Dict, List

SEVERITY_ORDER = {
    "INFO": 1,
    "MINOR": 2,
    "MAJOR": 3,
    "CRITICAL": 4,
    "BLOCKER": 5,
}

THRESHOLD_TO_SEVERITY = {
    # Software-quality model (UI-first names).
    "info": "INFO",
    "low": "MINOR",
    "medium": "MAJOR",
    "high": "CRITICAL",
    "blocker": "BLOCKER",
    # API/legacy aliases.
    "minor": "MINOR",
    "major": "MAJOR",
    "critical": "CRITICAL",
    # Backward-compatible alias.
    "all": "INFO",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host-url", required=True)
    parser.add_argument("--project-key", required=True)
    parser.add_argument("--changed-files", required=True)
    parser.add_argument(
        "--severity-threshold",
        default="high",
        help=(
            "Severity threshold: blocker|high|medium|low|info "
            "(aliases: critical|major|minor|all)"
        ),
    )
    parser.add_argument("--output-json", required=True)
    parser.add_argument("--output-md", required=True)
    parser.add_argument("--token", default="")
    parser.add_argument("--user", default="")
    parser.add_argument("--password", default="")
    return parser.parse_args()


def normalize_path(path: str) -> str:
    path = path.strip()
    if path.startswith("./"):
        path = path[2:]
    return os.path.normpath(path).replace("\\", "/")


def load_changed_files(path: str) -> set[str]:
    files: set[str] = set()
    with open(path, "r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            files.add(normalize_path(line))
    return files


def build_headers(token: str, user: str, password: str) -> Dict[str, str]:
    headers = {"Accept": "application/json"}
    if token:
        auth = f"{token}:"
    elif user or password:
        auth = f"{user}:{password}"
    else:
        auth = ""

    if auth:
        headers["Authorization"] = "Basic " + base64.b64encode(auth.encode("utf-8")).decode("ascii")
    return headers


def fetch_issues(host_url: str, project_key: str, headers: Dict[str, str]) -> List[Dict[str, Any]]:
    issues: List[Dict[str, Any]] = []
    page = 1

    while True:
        query = urllib.parse.urlencode(
            {
                "projectKeys": project_key,
                "statuses": "OPEN,CONFIRMED,REOPENED",
                "ps": "500",
                "p": str(page),
            }
        )
        url = f"{host_url.rstrip('/')}/api/issues/search?{query}"
        req = urllib.request.Request(url, headers=headers)

        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"SonarQube API error {exc.code}: {body}") from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(f"Cannot reach SonarQube at {host_url}: {exc}") from exc

        page_issues = payload.get("issues", [])
        issues.extend(page_issues)

        paging = payload.get("paging", {})
        total = int(paging.get("total", len(issues)))
        page_size = int(paging.get("pageSize", 500))
        if page * page_size >= total:
            break
        page += 1

    return issues


def issue_file_path(issue: Dict[str, Any]) -> str:
    component = issue.get("component", "")
    if ":" in component:
        component = component.split(":", 1)[1]
    return normalize_path(component)


def issue_line(issue: Dict[str, Any]) -> int:
    line = issue.get("line")
    if isinstance(line, int) and line > 0:
        return line
    text_range = issue.get("textRange", {})
    start_line = text_range.get("startLine")
    if isinstance(start_line, int) and start_line > 0:
        return start_line
    return 0


def filter_issues(raw_issues: List[Dict[str, Any]], changed_files: set[str], threshold: str) -> List[Dict[str, Any]]:
    threshold_sev = THRESHOLD_TO_SEVERITY[threshold]
    threshold_rank = SEVERITY_ORDER[threshold_sev]

    findings: List[Dict[str, Any]] = []
    for issue in raw_issues:
        severity = issue.get("severity", "INFO")
        rank = SEVERITY_ORDER.get(severity, 0)
        file_path = issue_file_path(issue)

        if rank < threshold_rank:
            continue
        if file_path not in changed_files:
            continue

        findings.append(
            {
                "key": issue.get("key", ""),
                "rule": issue.get("rule", ""),
                "type": issue.get("type", ""),
                "severity": severity,
                "message": issue.get("message", ""),
                "file": file_path,
                "line": issue_line(issue),
                "status": issue.get("status", ""),
                "effort": issue.get("effort", ""),
                "tags": issue.get("tags", []),
            }
        )

    findings.sort(
        key=lambda f: (
            -SEVERITY_ORDER.get(f["severity"], 0),
            f["file"],
            f["line"],
            f["key"],
        )
    )
    return findings


def write_markdown(path: str, project_key: str, threshold: str, changed_count: int, findings: List[Dict[str, Any]]) -> None:
    with open(path, "w", encoding="utf-8") as fh:
        fh.write("# SonarQube Findings (Changed Files)\n\n")
        fh.write(f"- Project: `{project_key}`\n")
        fh.write(f"- Severity threshold: `{threshold}`\n")
        fh.write(f"- Changed files scanned: `{changed_count}`\n")
        fh.write(f"- Findings: `{len(findings)}`\n\n")

        if not findings:
            fh.write("No findings at or above the selected threshold on changed files.\n")
            return

        fh.write("| Severity | File | Line | Rule | Message |\n")
        fh.write("|---|---|---:|---|---|\n")
        for item in findings:
            message = item["message"].replace("|", "\\|").replace("\n", " ")
            fh.write(
                f"| {item['severity']} | `{item['file']}` | {item['line'] or ''} | `{item['rule']}` | {message} |\n"
            )


def main() -> int:
    args = parse_args()
    threshold = args.severity_threshold.lower().strip()
    if threshold not in THRESHOLD_TO_SEVERITY:
        raise RuntimeError(
            "invalid severity threshold "
            f"'{args.severity_threshold}' "
            "(expected blocker|high|medium|low|info; aliases: critical|major|minor|all)"
        )

    changed_files = load_changed_files(args.changed_files)
    headers = build_headers(args.token, args.user, args.password)

    raw_issues = fetch_issues(args.host_url, args.project_key, headers)
    findings = filter_issues(raw_issues, changed_files, threshold)

    severity_counts = Counter(item["severity"] for item in findings)
    output = {
        "summary": {
            "project_key": args.project_key,
            "severity_threshold": threshold,
            "changed_files": len(changed_files),
            "findings": len(findings),
            "severity_counts": dict(sorted(severity_counts.items())),
        },
        "findings": findings,
    }

    os.makedirs(os.path.dirname(args.output_json) or ".", exist_ok=True)
    os.makedirs(os.path.dirname(args.output_md) or ".", exist_ok=True)

    with open(args.output_json, "w", encoding="utf-8") as fh:
        json.dump(output, fh, indent=2)
        fh.write("\n")

    write_markdown(
        path=args.output_md,
        project_key=args.project_key,
        threshold=threshold,
        changed_count=len(changed_files),
        findings=findings,
    )

    return 3 if findings else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
