"""MkDocs hook: point links that leave docs/ at the file on GitHub.

The pages under docs/ link to repository files outside it (specs/, AGENTS.md, tests/, ...), which
`task docs:check` resolves in the repository. The site holds only docs/, so the same relative link
would be broken there and `mkdocs build --strict` would refuse it. This hook rewrites such a link,
and only such a link, to the file or directory on the main branch of the repository (an image to
the raw file). A link that stays inside docs/ is left for MkDocs to validate. A link whose target
does not exist, or that would leave the repository, is left unchanged and logged as a warning, so
the strict build fails on it. Fenced code blocks and single-line code spans are not touched;
indented code blocks and code spans that wrap across lines are (none exist under docs/ today).
Tests: test_repo_links.py beside this file.
"""

import logging
import os
import posixpath
import re

log = logging.getLogger("mkdocs.hooks.repo_links")

_INLINE = re.compile(r"(\]\()([^)\s]+)((?:\s+(?:\"[^\"]*\"|'[^']*'))?\))")
_REFERENCE = re.compile(r"^( {0,3}\[[^\]^][^\]]*\]:\s*)(\S+)(.*)$")
_FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})")
_CODE_SPAN = re.compile(r"(`+)(?:(?!\1).)+?\1")
_SCHEME = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")

_state = {}


def on_config(config):
    repo_url = (config.get("repo_url") or "").rstrip("/")
    if not repo_url:
        raise ValueError("repo_links hook: repo_url must be set in mkdocs.yml")
    _state["repo_url"] = repo_url
    _state["root"] = os.path.dirname(os.path.abspath(config.config_file_path))
    _state["docs"] = os.path.relpath(config.docs_dir, _state["root"]).replace(os.sep, "/")
    return config


def _rewrite(target, page_src, image=False):
    if not target or target.startswith(("#", "/")) or _SCHEME.match(target):
        return target
    path, sep, fragment = target.partition("#")
    path, qsep, query = path.partition("?")
    if not path:
        return target
    repo_path = posixpath.normpath(posixpath.join(_state["docs"], posixpath.dirname(page_src), path))
    if repo_path == _state["docs"] or repo_path.startswith(_state["docs"] + "/"):
        return target
    full = os.path.join(_state["root"], repo_path)
    if repo_path == ".." or repo_path.startswith("../") or not os.path.exists(full):
        log.warning("Doc file '%s' links to '%s', which is not a file in the repository", page_src, target)
        return target
    kind = "raw" if image else "tree" if os.path.isdir(full) else "blob"
    return f"{_state['repo_url']}/{kind}/main/{repo_path}{qsep}{query}{sep}{fragment}"


def _outside_code(line, fix):
    out, last = [], 0
    for span in _CODE_SPAN.finditer(line):
        out.append(fix(line[last:span.start()]))
        out.append(span.group(0))
        last = span.end()
    out.append(fix(line[last:]))
    return "".join(out)


def on_page_markdown(markdown, page, config, files):
    page_src = page.file.src_uri

    def fix(text):
        def one(m):
            # An image when the bracket that opens this link's text follows a "!" on this line;
            # link text may wrap from the previous line, so the opening bracket may be absent.
            opening = text.rfind("[", 0, m.start())
            image = opening > 0 and text[opening - 1] == "!"
            return m.group(1) + _rewrite(m.group(2), page_src, image) + m.group(3)
        return _INLINE.sub(one, text)

    lines, fence = [], None
    for line in markdown.split("\n"):
        opened = _FENCE.match(line)
        if fence is not None:
            if opened and opened.group(1)[0] == fence[0] and len(opened.group(1)) >= len(fence):
                fence = None
            lines.append(line)
            continue
        if opened:
            fence = opened.group(1)
            lines.append(line)
            continue
        reference = _REFERENCE.match(line)
        if reference:
            line = reference.group(1) + _rewrite(reference.group(2), page_src) + reference.group(3)
        else:
            line = _outside_code(line, fix)
        lines.append(line)
    return "\n".join(lines)
