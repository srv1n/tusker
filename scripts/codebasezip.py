#!/usr/bin/env python3
"""Create a small, source-only handoff archive from the current Git tree.

The archive deliberately contains reviewable source/config/test files only.
Git's NUL-delimited path output makes this safe for unusual (including
newline-containing) filenames, while the explicit allowlist prevents ignored
or force-added binaries and generated assets from entering the handoff.
"""

from __future__ import annotations

import argparse
import fnmatch
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import sys
import tempfile
import zipfile


# Repository-level directories that are never source handoff material. These
# are deliberately root-scoped: `internal/serve/ui/src/features/docs/` is
# source code and must not be confused with the repository's top-level docs/.
EXCLUDED_ROOT_DIRS = frozenset(
    {
        ".git",
        ".tools",
        ".tusker",
        ".chatgpt-handoff",
        ".tusker-build",
        ".tusker-runtime",
        ".tusker-state",
        ".tusker-worktrees",
        "artifacts",
        "architect",
        "docs",
        "feedback",
        "research",
        "uploads",
        "vendor",
    }
)

# Build/cache names can occur at any depth (for example a UI package's
# node_modules or an embedded frontend's dist/). Matching by component avoids
# shell glob edge cases such as a pattern that misses root-level dist/.
EXCLUDED_ANYWHERE_DIRS = frozenset(
    {
        "node_modules",
        ".build",
        "build",
        "coverage",
        "dist",
        "out",
        ".vite",
        ".astro",
        ".cache",
        ".turbo",
        ".next",
        ".svelte-kit",
        "target",
        "__pycache__",
        ".pytest_cache",
        ".mypy_cache",
    }
)

EXCLUDED_BASENAMES = frozenset({".DS_Store"})

# These suffixes are rejected even if a future force-added file has a name
# which otherwise looks like a source file. SVG is intentionally retained as a
# textual source asset; raster/font/model/audio/video assets are not.
EXCLUDED_SUFFIXES = (
    ".zip",
    ".tar",
    ".tar.gz",
    ".tgz",
    ".gz",
    ".bz2",
    ".xz",
    ".7z",
    ".bin",
    ".exe",
    ".dmg",
    ".pkg",
    ".wasm",
    ".so",
    ".dylib",
    ".dll",
    ".a",
    ".o",
    ".obj",
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".webp",
    ".ico",
    ".icns",
    ".woff",
    ".woff2",
    ".ttf",
    ".otf",
    ".onnx",
    ".wav",
    ".flac",
    ".mp3",
    ".mp4",
    ".mov",
    ".webm",
    ".log",
)

ALLOWED_BASENAMES = frozenset(
    {
        "Makefile",
        "Dockerfile",
        "Justfile",
        "go.mod",
        "go.sum",
        "package.json",
        "bun.lock",
        "Package.swift",
        "Package.resolved",
        "Cargo.toml",
        "Cargo.lock",
    }
)

ALLOWED_SUFFIXES = (
    ".go",
    ".ts",
    ".tsx",
    ".js",
    ".jsx",
    ".mjs",
    ".cjs",
    ".css",
    ".scss",
    ".html",
    ".swift",
    ".c",
    ".h",
    ".m",
    ".mm",
    ".sh",
    ".bash",
    ".zsh",
    ".py",
    ".rs",
    ".sql",
    ".proto",
    ".graphql",
    ".gql",
    ".svg",
    ".json",
    ".toml",
    ".yaml",
    ".yml",
    ".plist",
    ".entitlements",
    ".mk",
    ".env.example",
)


def repo_root() -> Path:
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return Path(result.stdout.strip()).resolve()


def git_paths(root: Path) -> list[Path]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return [Path(os.fsdecode(raw)) for raw in result.stdout.split(b"\0") if raw]


def excluded(rel: Path, output: Path) -> bool:
    parts = rel.parts
    if parts and parts[0] in EXCLUDED_ROOT_DIRS:
        return True
    if len(parts) >= 2 and parts[:2] == (".claude", "worktrees"):
        return True
    if any(part in EXCLUDED_ANYWHERE_DIRS for part in parts[:-1]):
        return True
    if rel.name in EXCLUDED_BASENAMES:
        return True
    lower_name = rel.name.lower()
    if any(lower_name.endswith(suffix) for suffix in EXCLUDED_SUFFIXES):
        return True
    # The output path is excluded by identity even when a caller deliberately
    # chooses a non-.zip name or a directory outside the normal artifacts/ path.
    if output is not None and rel == output:
        return True
    if rel.name in ALLOWED_BASENAMES:
        return False
    if any(fnmatch.fnmatchcase(rel.name, f"*{suffix}") for suffix in ALLOWED_SUFFIXES):
        return False
    return True


def safe_regular_file(path: Path) -> int | None:
    """Open without following symlinks and return an fd for a regular file."""

    if path.is_symlink():
        return None
    nofollow = getattr(os, "O_NOFOLLOW", 0)
    try:
        fd = os.open(path, os.O_RDONLY | nofollow)
    except OSError:
        return None
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            os.close(fd)
            return None
    except OSError:
        os.close(fd)
        return None
    return fd


def write_archive(root: Path, output: Path) -> tuple[int, int]:
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        output_rel: Path | None = output.relative_to(root)
    except ValueError:
        output_rel = None
    candidates: list[Path] = []
    skipped_symlinks = 0
    for rel in git_paths(root):
        # Git paths are POSIX-relative even on Windows; normalize only after
        # decoding, and reject traversal rather than allowing it into the zip.
        rel_posix = PurePosixPath(rel.as_posix())
        if rel_posix.is_absolute() or ".." in rel_posix.parts:
            continue
        rel = Path(*rel_posix.parts)
        candidate = root / rel
        if candidate.is_symlink():
            skipped_symlinks += 1
            continue
        path = candidate.resolve(strict=False)
        try:
            path.relative_to(root)
        except ValueError:
            continue
        if excluded(rel, output_rel):
            continue
        if not path.is_file():
            continue
        candidates.append(rel)

    candidates = sorted(set(candidates), key=lambda item: item.as_posix())
    if not candidates:
        raise SystemExit("No files matched the codebase archive filter.")

    fd, temporary_name = tempfile.mkstemp(
        prefix=f".{output.name}.", suffix=".tmp", dir=output.parent
    )
    os.close(fd)
    count = 0
    total_bytes = 0
    try:
        with zipfile.ZipFile(
            temporary_name,
            mode="w",
            compression=zipfile.ZIP_DEFLATED,
            compresslevel=9,
        ) as archive:
            for rel in candidates:
                path = root / rel
                fd = safe_regular_file(path)
                if fd is None:
                    skipped_symlinks += 1
                    continue
                try:
                    total_size = os.fstat(fd).st_size
                    info = zipfile.ZipInfo(rel.as_posix(), date_time=(1980, 1, 1, 0, 0, 0))
                    info.create_system = 3
                    info.external_attr = (0o100644 & 0xFFFF) << 16
                    info.compress_type = zipfile.ZIP_DEFLATED
                    with os.fdopen(fd, "rb") as source, archive.open(info, "w") as target:
                        shutil.copyfileobj(source, target, length=1024 * 1024)
                    total_bytes += total_size
                    count += 1
                except Exception:
                    try:
                        os.close(fd)
                    except OSError:
                        pass
                    raise
        os.replace(temporary_name, output)
    except Exception:
        try:
            os.unlink(temporary_name)
        except FileNotFoundError:
            pass
        raise
    if skipped_symlinks:
        print(f"Skipped {skipped_symlinks} symlink(s).", file=sys.stderr)
    return count, total_bytes


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, help="archive path")
    args = parser.parse_args()
    root = repo_root()
    output = Path(args.output)
    if not output.is_absolute():
        output = Path.cwd() / output
    count, total_bytes = write_archive(root, output)
    print(f"Created {output} ({count} files, {total_bytes} source bytes)")
    if os.environ.get("CI") != "true" and sys.platform == "darwin":
        subprocess.run(["open", str(output.parent)], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
