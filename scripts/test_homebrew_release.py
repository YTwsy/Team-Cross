#!/usr/bin/env python3

import json
import pathlib
import sys
import tempfile
import unittest


sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from homebrew_release import (  # noqa: E402
    DEVELOPMENT,
    RC,
    STABLE,
    channel_for_version,
    render_bundle,
    stage_bundle,
    update_release_notes,
    validate_bundle,
)


class HomebrewReleaseTest(unittest.TestCase):
    def render(self, root: pathlib.Path, version: str) -> pathlib.Path:
        output = root / "homebrew-teamcross"
        render_bundle(
            output,
            version=version,
            commit="a" * 40,
            base_url=f"https://github.com/YTwsy/Team-Cross/releases/download/v{version}",
            cli_sha="b" * 64,
            dmg_sha="c" * 64,
            developer_id_signed=False,
            notarized=False,
        )
        return output

    def test_channel_selection(self) -> None:
        self.assertEqual(channel_for_version("1.2.3"), STABLE)
        self.assertEqual(channel_for_version("1.2.3-rc.4"), RC)
        self.assertEqual(channel_for_version("1.2.3-dev"), DEVELOPMENT)

    def test_stable_and_rc_render_distinct_definitions(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            stable = self.render(root / "stable", "1.2.3")
            rc = self.render(root / "rc", "1.2.4-rc.2")
            self.assertTrue((stable / STABLE.formula_path).is_file())
            self.assertTrue((stable / STABLE.cask_path).is_file())
            self.assertTrue((rc / RC.formula_path).is_file())
            self.assertTrue((rc / RC.cask_path).is_file())
            self.assertIn("class TeamcrossRc < Formula", (rc / RC.formula_path).read_text())
            self.assertIn('cask "team-cross@rc" do', (rc / RC.cask_path).read_text())
            self.assertIn("teamcross-rc", (rc / "README.md").read_text())

    def test_stage_preserves_the_other_channel(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            bundle = self.render(root / "bundle", "1.2.4-rc.2")
            tap = root / "tap"
            (tap / ".git").mkdir(parents=True)
            stable = tap / STABLE.formula_path
            stable.parent.mkdir(parents=True)
            stable.write_text("keep stable\n")
            metadata, channel = stage_bundle(bundle, tap)
            self.assertEqual(metadata["version"], "1.2.4-rc.2")
            self.assertEqual(channel, RC)
            self.assertEqual(stable.read_text(), "keep stable\n")
            self.assertTrue((tap / RC.formula_path).is_file())

    def test_stage_refuses_channel_rollback(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            newer = self.render(root / "newer", "1.2.4-rc.3")
            older = self.render(root / "older", "1.2.4-rc.2")
            tap = root / "tap"
            (tap / ".git").mkdir(parents=True)
            stage_bundle(newer, tap)
            with self.assertRaisesRegex(ValueError, "refusing to roll back rc channel"):
                stage_bundle(older, tap)

    def test_stage_refuses_conflicting_same_version_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            bundle = self.render(root / "bundle", "1.2.3")
            tap = root / "tap"
            (tap / ".git").mkdir(parents=True)
            stage_bundle(bundle, tap)
            metadata_path = tap / STABLE.metadata_path
            metadata = json.loads(metadata_path.read_text())
            metadata["source"]["commit"] = "f" * 40
            metadata_path.write_text(json.dumps(metadata))
            with self.assertRaisesRegex(ValueError, "refusing to replace conflicting metadata"):
                stage_bundle(bundle, tap)

    def test_extra_bundle_file_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            bundle = self.render(pathlib.Path(temporary), "1.2.3")
            (bundle / "unexpected.txt").write_text("no\n")
            with self.assertRaisesRegex(ValueError, "unexpected Homebrew bundle files"):
                validate_bundle(bundle)

    def test_unmarked_output_is_not_replaced(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            output = root / "homebrew-teamcross"
            output.mkdir()
            preserved = output / "preserved.txt"
            preserved.write_text("keep\n")
            with self.assertRaisesRegex(ValueError, "refusing to replace an unmarked Homebrew directory"):
                render_bundle(
                    output,
                    version="1.2.3",
                    commit="a" * 40,
                    base_url="https://github.com/YTwsy/Team-Cross/releases/download/v1.2.3",
                    cli_sha="b" * 64,
                    dmg_sha="c" * 64,
                    developer_id_signed=False,
                    notarized=False,
                )
            self.assertEqual(preserved.read_text(), "keep\n")

    def test_development_bundle_is_not_publishable(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            bundle = self.render(pathlib.Path(temporary), "1.2.3-dev")
            with self.assertRaisesRegex(ValueError, "development Homebrew bundles cannot be published"):
                validate_bundle(bundle)

    def test_release_notes_homebrew_section_is_idempotent(self) -> None:
        metadata = {"version": "1.2.3-rc.1"}
        first = update_release_notes(
            "Release body\n",
            metadata,
            "https://github.com/YTwsy/homebrew-teamcross/pull/12",
            "d" * 40,
        )
        second = update_release_notes(
            first,
            metadata,
            "https://github.com/YTwsy/homebrew-teamcross/pull/13",
            "e" * 40,
        )
        self.assertEqual(second.count("<!-- teamcross-homebrew:start -->"), 1)
        self.assertNotIn("pull/12", second)
        self.assertIn("pull/13", second)
        self.assertIn("team-cross@rc", second)


if __name__ == "__main__":
    unittest.main()
