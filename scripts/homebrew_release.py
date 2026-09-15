#!/usr/bin/env python3
"""Render and stage deterministic Team Cross Homebrew channel updates."""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import shutil
from dataclasses import dataclass


ROOT = pathlib.Path(__file__).resolve().parents[1]
REPOSITORY = "YTwsy/Team-Cross"
STABLE_VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
RC_VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*")
SHA256 = re.compile(r"[0-9a-f]{64}")
COMMIT = re.compile(r"[0-9a-f]{40}")
HOMEBREW_NOTES_START = "<!-- teamcross-homebrew:start -->"
HOMEBREW_NOTES_END = "<!-- teamcross-homebrew:end -->"


@dataclass(frozen=True)
class Channel:
    name: str
    formula_token: str
    formula_class: str
    formula_path: str
    cask_token: str
    cask_path: str
    metadata_path: str


STABLE = Channel(
    name="stable",
    formula_token="teamcross",
    formula_class="Teamcross",
    formula_path="Formula/teamcross.rb",
    cask_token="team-cross",
    cask_path="Casks/team-cross.rb",
    metadata_path="Versions/stable.json",
)
RC = Channel(
    name="rc",
    formula_token="teamcross-rc",
    formula_class="TeamcrossRc",
    formula_path="Formula/teamcross-rc.rb",
    cask_token="team-cross@rc",
    cask_path="Casks/team-cross@rc.rb",
    metadata_path="Versions/rc.json",
)
DEVELOPMENT = Channel(
    name="development",
    formula_token=STABLE.formula_token,
    formula_class=STABLE.formula_class,
    formula_path=STABLE.formula_path,
    cask_token=STABLE.cask_token,
    cask_path=STABLE.cask_path,
    metadata_path="Versions/development.json",
)


def channel_for_version(version: str) -> Channel:
    if STABLE_VERSION.fullmatch(version):
        return STABLE
    if RC_VERSION.fullmatch(version):
        return RC
    return DEVELOPMENT


def sha256(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _render(template: pathlib.Path, values: dict[str, str]) -> str:
    text = template.read_text()
    for key, value in values.items():
        text = text.replace(f"@{key}@", value)
    unresolved = sorted(set(re.findall(r"@[A-Z][A-Z0-9_]*@", text)))
    if unresolved:
        raise ValueError(f"unresolved Homebrew template fields: {unresolved}")
    return text


def tap_readme() -> str:
    return """# Team Cross Homebrew tap

稳定版与 RC 使用独立通道；每种安装方式都提供 `teamcross` 命令，因此四种定义互斥，不能同时安装。

## 稳定版

App 与命令行：

```sh
brew install --cask YTwsy/teamcross/team-cross
```

独立命令行：

```sh
brew install YTwsy/teamcross/teamcross
```

## RC 候选版

App 与命令行：

```sh
brew install --cask YTwsy/teamcross/team-cross@rc
```

独立命令行：

```sh
brew install YTwsy/teamcross/teamcross-rc
```

RC 只在用户显式安装上述 RC 定义时生效，不会把稳定通道的普通 `brew upgrade` 自动切换到候选版。

切换渠道前退出 Team Cross，并通过原渠道卸载。Formula 使用 `brew uninstall --formula --force <名称>`，Cask 使用 `brew uninstall --cask <名称>`；协作数据和工作目录会保留。首次打开、未公证构建的系统提示与完整说明见 [Team Cross](https://github.com/YTwsy/Team-Cross#安装)。
"""


def render_bundle(
    output: pathlib.Path,
    *,
    version: str,
    commit: str,
    base_url: str,
    cli_sha: str,
    dmg_sha: str,
    developer_id_signed: bool,
    notarized: bool,
) -> dict[str, object]:
    channel = channel_for_version(version)
    if not COMMIT.fullmatch(commit):
        raise ValueError(f"invalid source commit: {commit}")
    for label, value in (("CLI", cli_sha), ("DMG", dmg_sha)):
        if not SHA256.fullmatch(value):
            raise ValueError(f"invalid {label} SHA-256: {value}")
    if any(character in base_url for character in ('"', "\n", "\r", "\\")):
        raise ValueError("invalid release asset base URL")

    marker = output / ".teamcross-homebrew-output"
    if output.exists():
        owned_build_directory = (
            output.name == "homebrew-teamcross"
            and (output.parent / ".teamcross-build-output").is_file()
        )
        if not marker.is_file() and not owned_build_directory:
            raise ValueError(f"refusing to replace an unmarked Homebrew directory: {output}")
        shutil.rmtree(output)
    output.mkdir(parents=True)
    marker.write_text("Team Cross generated Homebrew output\n")

    other = RC if channel.name != "rc" else STABLE
    cli_name = f"teamcross-{version}-darwin-arm64.tar.gz"
    dmg_name = f"Team-Cross-{version}-arm64.dmg"
    values = {
        "VERSION": version,
        "BASE_URL": base_url,
        "CLI_SHA": cli_sha,
        "DMG_SHA": dmg_sha,
        "FORMULA_CLASS": channel.formula_class,
        "FORMULA_TOKEN": channel.formula_token,
        "OTHER_FORMULA_TOKEN": other.formula_token,
        "CASK_TOKEN": channel.cask_token,
        "OTHER_CASK_TOKEN": other.cask_token,
    }
    formula = output / channel.formula_path
    cask = output / channel.cask_path
    formula.parent.mkdir(parents=True)
    cask.parent.mkdir(parents=True)
    formula.write_text(_render(ROOT / "packaging/homebrew/teamcross.rb.in", values))
    cask.write_text(_render(ROOT / "packaging/homebrew/team-cross.rb.in", values))
    (output / "README.md").write_text(tap_readme())

    metadata: dict[str, object] = {
        "schemaVersion": 1,
        "channel": channel.name,
        "version": version,
        "tag": f"v{version}",
        "source": {"repository": REPOSITORY, "commit": commit},
        "architecture": "arm64",
        "minimumMacOS": "14.0",
        "developerIDSigned": developer_id_signed,
        "notarized": notarized,
        "artifacts": {
            "cli": {
                "name": cli_name,
                "url": f"{base_url}/{cli_name}",
                "sha256": cli_sha,
            },
            "dmg": {
                "name": dmg_name,
                "url": f"{base_url}/{dmg_name}",
                "sha256": dmg_sha,
            },
        },
        "definitions": {
            "formula": channel.formula_path,
            "cask": channel.cask_path,
        },
    }
    metadata_path = output / channel.metadata_path
    metadata_path.parent.mkdir(parents=True)
    metadata_path.write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
    validate_bundle(output, publishable=channel.name != "development")
    return metadata


def render_from_release(release: pathlib.Path, output: pathlib.Path) -> dict[str, object]:
    manifest = json.loads((release / "release.json").read_text())
    version = manifest.get("version")
    commit = manifest.get("commit")
    artifacts = manifest.get("artifacts")
    if not isinstance(version, str) or not isinstance(commit, str) or not isinstance(artifacts, dict):
        raise ValueError("release.json is missing version, commit, or artifacts")
    if manifest.get("dirty") is not False:
        raise ValueError("Homebrew publishing requires a clean release manifest")
    if manifest.get("architecture") != "arm64" or manifest.get("minimumMacOS") != "14.0":
        raise ValueError("release.json platform is not the supported Homebrew target")
    cli_name = f"teamcross-{version}-darwin-arm64.tar.gz"
    dmg_name = f"Team-Cross-{version}-arm64.dmg"
    if set(artifacts) != {cli_name, dmg_name}:
        raise ValueError("release.json does not contain the exact CLI and DMG artifact set")
    for name in (cli_name, dmg_name):
        path = release / name
        expected = artifacts[name]
        if not path.is_file() or sha256(path) != expected:
            raise ValueError(f"public release artifact does not match release.json: {name}")
    return render_bundle(
        output,
        version=version,
        commit=commit,
        base_url=f"https://github.com/{REPOSITORY}/releases/download/v{version}",
        cli_sha=artifacts[cli_name],
        dmg_sha=artifacts[dmg_name],
        developer_id_signed=bool(manifest.get("developerIDSigned")),
        notarized=bool(manifest.get("notarized")),
    )


def load_bundle(bundle: pathlib.Path) -> tuple[dict[str, object], Channel]:
    metadata_paths = sorted((bundle / "Versions").glob("*.json"))
    if len(metadata_paths) != 1:
        raise ValueError(f"Homebrew bundle must contain one channel metadata file, found {len(metadata_paths)}")
    metadata = json.loads(metadata_paths[0].read_text())
    version = metadata.get("version")
    if not isinstance(version, str):
        raise ValueError("Homebrew metadata version is missing")
    channel = channel_for_version(version)
    if metadata.get("channel") != channel.name or metadata_paths[0].relative_to(bundle).as_posix() != channel.metadata_path:
        raise ValueError("Homebrew metadata channel does not match its version or path")
    return metadata, channel


def validate_bundle(bundle: pathlib.Path, *, publishable: bool = True) -> tuple[dict[str, object], Channel]:
    metadata, channel = load_bundle(bundle)
    if publishable and channel.name == "development":
        raise ValueError("development Homebrew bundles cannot be published")

    version = metadata["version"]
    tag = metadata.get("tag")
    source = metadata.get("source")
    artifacts = metadata.get("artifacts")
    definitions = metadata.get("definitions")
    if tag != f"v{version}":
        raise ValueError("Homebrew metadata tag does not match version")
    if not isinstance(source, dict) or source.get("repository") != REPOSITORY or not COMMIT.fullmatch(str(source.get("commit", ""))):
        raise ValueError("Homebrew metadata source is invalid")
    if metadata.get("architecture") != "arm64" or metadata.get("minimumMacOS") != "14.0":
        raise ValueError("Homebrew metadata platform is invalid")
    if not isinstance(artifacts, dict) or not isinstance(definitions, dict):
        raise ValueError("Homebrew metadata artifact or definition map is invalid")
    if definitions != {"formula": channel.formula_path, "cask": channel.cask_path}:
        raise ValueError("Homebrew definition paths do not match the channel")

    cli = artifacts.get("cli")
    dmg = artifacts.get("dmg")
    if not isinstance(cli, dict) or not isinstance(dmg, dict):
        raise ValueError("Homebrew artifact metadata is invalid")
    for artifact in (cli, dmg):
        if not SHA256.fullmatch(str(artifact.get("sha256", ""))):
            raise ValueError("Homebrew artifact SHA-256 is invalid")
        if artifact.get("url") != f"https://github.com/{REPOSITORY}/releases/download/{tag}/{artifact.get('name')}":
            raise ValueError("Homebrew artifact URL is not the canonical GitHub Release URL")

    formula_path = bundle / channel.formula_path
    cask_path = bundle / channel.cask_path
    formula = formula_path.read_text()
    cask = cask_path.read_text()
    expected_formula = [
        f"class {channel.formula_class} < Formula",
        f'url "{cli["url"]}"',
        f'version "{version}"',
        f'sha256 "{cli["sha256"]}"',
    ]
    expected_cask = [
        f'cask "{channel.cask_token}" do',
        f'url "{dmg["url"]}"',
        f'version "{version}"',
        f'sha256 "{dmg["sha256"]}"',
    ]
    for line in expected_formula:
        if line not in formula:
            raise ValueError(f"Formula is missing expected content: {line}")
    for line in expected_cask:
        if line not in cask:
            raise ValueError(f"Cask is missing expected content: {line}")

    expected_files = {
        ".teamcross-homebrew-output",
        "README.md",
        channel.formula_path,
        channel.cask_path,
        channel.metadata_path,
    }
    actual_files = {
        path.relative_to(bundle).as_posix()
        for path in bundle.rglob("*")
        if path.is_file() or path.is_symlink()
    }
    if actual_files != expected_files:
        raise ValueError(f"unexpected Homebrew bundle files: {sorted(actual_files ^ expected_files)}")
    if any(path.is_symlink() for path in bundle.rglob("*")):
        raise ValueError("Homebrew bundle must not contain symlinks")
    return metadata, channel


def _version_key(version: str, channel: Channel) -> tuple[int, ...]:
    if channel == STABLE:
        match = re.fullmatch(r"([0-9]+)\.([0-9]+)\.([0-9]+)", version)
    elif channel == RC:
        match = re.fullmatch(r"([0-9]+)\.([0-9]+)\.([0-9]+)-rc\.([1-9][0-9]*)", version)
    else:
        match = None
    if match is None:
        raise ValueError(f"invalid {channel.name} channel version: {version}")
    return tuple(int(part) for part in match.groups())


def _check_tap_version(tap: pathlib.Path, metadata: dict[str, object], channel: Channel) -> None:
    current_path = tap / channel.metadata_path
    if not current_path.exists():
        return
    current = json.loads(current_path.read_text())
    current_version = current.get("version")
    target_version = metadata.get("version")
    if not isinstance(current_version, str) or not isinstance(target_version, str):
        raise ValueError(f"existing {channel.name} channel metadata has no valid version")
    if current.get("channel") != channel.name or channel_for_version(current_version) != channel:
        raise ValueError(f"existing {channel.name} channel metadata is inconsistent")
    current_key = _version_key(current_version, channel)
    target_key = _version_key(target_version, channel)
    if current_key > target_key:
        raise ValueError(f"refusing to roll back {channel.name} channel from {current_version} to {target_version}")
    if current_key == target_key and current != metadata:
        raise ValueError(f"refusing to replace conflicting metadata for {channel.name} {target_version}")


def stage_bundle(bundle: pathlib.Path, tap: pathlib.Path) -> tuple[dict[str, object], Channel]:
    metadata, channel = validate_bundle(bundle)
    if not (tap / ".git").exists():
        raise ValueError(f"tap checkout is not a Git repository: {tap}")
    _check_tap_version(tap, metadata, channel)
    for relative in ("README.md", channel.formula_path, channel.cask_path, channel.metadata_path):
        source = bundle / relative
        destination = tap / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination)
    return metadata, channel


def update_release_notes(body: str, metadata: dict[str, object], pr_url: str, tap_commit: str) -> str:
    version = str(metadata.get("version", ""))
    channel = channel_for_version(version)
    if channel.name == "development":
        raise ValueError("development releases do not have public Homebrew notes")
    if not re.fullmatch(r"https://github\.com/YTwsy/homebrew-teamcross/pull/[1-9][0-9]*", pr_url):
        raise ValueError("invalid Homebrew tap pull request URL")
    if not COMMIT.fullmatch(tap_commit):
        raise ValueError("invalid Homebrew tap commit")
    if channel.name == "stable":
        label = "稳定通道"
        formula_command = "brew install YTwsy/teamcross/teamcross"
        cask_command = "brew install --cask YTwsy/teamcross/team-cross"
    else:
        label = "RC 独立通道"
        formula_command = "brew install YTwsy/teamcross/teamcross-rc"
        cask_command = "brew install --cask YTwsy/teamcross/team-cross@rc"
    section = f"""{HOMEBREW_NOTES_START}
## Homebrew

{label}已发布并通过公共 tap 安装验证；Formula 与 Cask 选择一种：

```sh
{formula_command}
```

```sh
{cask_command}
```

- Tap PR：{pr_url}
- Tap commit：`{tap_commit}`

稳定版与 RC 通道互斥；切换前先通过原渠道卸载，协作数据和工作目录会保留。
{HOMEBREW_NOTES_END}"""
    if HOMEBREW_NOTES_START in body or HOMEBREW_NOTES_END in body:
        pattern = re.compile(
            re.escape(HOMEBREW_NOTES_START) + r".*?" + re.escape(HOMEBREW_NOTES_END),
            re.DOTALL,
        )
        if len(pattern.findall(body)) != 1:
            raise ValueError("release notes contain malformed Homebrew markers")
        return pattern.sub(section, body).rstrip() + "\n"
    return body.rstrip() + "\n\n" + section + "\n"


def _command() -> None:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    validate = subparsers.add_parser("validate")
    validate.add_argument("bundle", type=pathlib.Path)
    render = subparsers.add_parser("render")
    render.add_argument("release", type=pathlib.Path)
    render.add_argument("output", type=pathlib.Path)
    notes = subparsers.add_parser("notes")
    notes.add_argument("body", type=pathlib.Path)
    notes.add_argument("metadata", type=pathlib.Path)
    notes.add_argument("pr_url")
    notes.add_argument("tap_commit")
    notes.add_argument("output", type=pathlib.Path)
    stage = subparsers.add_parser("stage")
    stage.add_argument("bundle", type=pathlib.Path)
    stage.add_argument("tap", type=pathlib.Path)
    args = parser.parse_args()
    if args.command == "notes":
        metadata = json.loads(args.metadata.read_text())
        args.output.write_text(update_release_notes(args.body.read_text(), metadata, args.pr_url, args.tap_commit))
        channel = channel_for_version(str(metadata["version"]))
    elif args.command == "render":
        metadata = render_from_release(args.release.resolve(), args.output.resolve())
        channel = channel_for_version(str(metadata["version"]))
    elif args.command == "validate":
        metadata, channel = validate_bundle(args.bundle.resolve())
    else:
        metadata, channel = stage_bundle(args.bundle.resolve(), args.tap.resolve())
    print(json.dumps({"version": metadata["version"], "tag": metadata["tag"], "channel": channel.name}))


if __name__ == "__main__":
    _command()
