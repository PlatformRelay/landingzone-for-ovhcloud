"""Tests of the repo_links MkDocs hook. Run from the repository root:

    python -m unittest discover -s overrides/hooks -p 'test_*.py'

They need only the standard library; the docs workflow runs them before the site build.
"""

import importlib.util
import os
import tempfile
import unittest
from types import SimpleNamespace

_SPEC = importlib.util.spec_from_file_location(
    "repo_links", os.path.join(os.path.dirname(os.path.abspath(__file__)), "repo_links.py"))
repo_links = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(repo_links)

REPO = "https://github.com/PlatformRelay/landingzone-for-ovhcloud"


class _Config(SimpleNamespace):
    def get(self, key, default=None):
        return getattr(self, key, default)


class RepoLinksTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        for path in ("docs/reference/glossary.md", "docs/README.md", "AGENTS.md",
                     "specs/005/spec.md", "docs/assets/logo.svg", "art/logo.svg"):
            full = os.path.join(self.root, path)
            os.makedirs(os.path.dirname(full), exist_ok=True)
            open(full, "w").close()
        config = _Config(repo_url=REPO + "/", config_file_path=os.path.join(self.root, "mkdocs.yml"),
                         docs_dir=os.path.join(self.root, "docs"))
        repo_links.on_config(config)
        self.config = config

    def render(self, markdown, src_uri="reference/glossary.md"):
        page = SimpleNamespace(file=SimpleNamespace(src_uri=src_uri))
        return repo_links.on_page_markdown(markdown, page, self.config, None)

    def test_link_inside_docs_is_left_for_mkdocs(self):
        self.assertEqual(self.render("[g](../README.md#start)"), "[g](../README.md#start)")

    def test_link_leaving_docs_points_at_the_file_on_main_with_its_fragment(self):
        self.assertEqual(self.render("[s](../../specs/005/spec.md#known-deviations)"),
                         f"[s]({REPO}/blob/main/specs/005/spec.md#known-deviations)")

    def test_directory_uses_tree_and_keeps_its_query(self):
        self.assertEqual(self.render("[d](../../specs/?x=1#a)"), f"[d]({REPO}/tree/main/specs?x=1#a)")

    def test_link_text_wrapped_from_the_previous_line_is_rewritten(self):
        self.assertEqual(self.render("see the [working\nrules](../../AGENTS.md)."),
                         f"see the [working\nrules]({REPO}/blob/main/AGENTS.md).")

    def test_reference_definition_is_rewritten(self):
        self.assertEqual(self.render("[a]: ../../AGENTS.md"), f"[a]: {REPO}/blob/main/AGENTS.md")

    def test_image_leaving_docs_points_at_the_raw_file(self):
        self.assertEqual(self.render("![logo](../../art/logo.svg)"), f"![logo]({REPO}/raw/main/art/logo.svg)")

    def test_image_inside_docs_is_left_for_mkdocs(self):
        self.assertEqual(self.render("![logo](../assets/logo.svg)"), "![logo](../assets/logo.svg)")

    def test_fenced_code_and_code_spans_are_untouched(self):
        text = "```md\n[a](../../AGENTS.md)\n```\n`[a](../../AGENTS.md)` and [a](../../AGENTS.md)"
        self.assertEqual(self.render(text),
                         f"```md\n[a](../../AGENTS.md)\n```\n`[a](../../AGENTS.md)` and [a]({REPO}/blob/main/AGENTS.md)")

    def test_missing_target_is_a_warning_and_stays_relative(self):
        with self.assertLogs("mkdocs.hooks.repo_links", level="WARNING") as logs:
            self.assertEqual(self.render("[x](../../NOPE.md#a)"), "[x](../../NOPE.md#a)")
        self.assertIn("NOPE.md", logs.output[0])

    def test_link_escaping_the_repository_stays_relative(self):
        self.assertEqual(self.render("[x](../../../outside.md)"), "[x](../../../outside.md)")

    def test_external_and_anchor_links_are_untouched(self):
        text = "[e](https://example.org/a) [h](#here) [m](mailto:a@example.org)"
        self.assertEqual(self.render(text), text)


if __name__ == "__main__":
    unittest.main()
