#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Build the macOS app classes with an isolated regression-test entry point."""

import argparse
import io
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--revision", help="Test an earlier Git revision with the current regression entry point")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    artifacts = root / "Artifacts"
    if not (artifacts / "WireGuardKitGo.xcframework").is_dir():
        parser.error("Build the Go XCFramework before running the editor tests")

    output = root / "build/tests/macOS-editor"
    output.mkdir(parents=True, exist_ok=True)
    # The Xcode project has a source-formatting phase; build a disposable copy.
    with tempfile.TemporaryDirectory(prefix="source-", dir=output) as scratch:
        source = Path(scratch)
        if args.revision:
            archive = subprocess.check_output(["git", "archive", args.revision], cwd=root)
            with tarfile.open(fileobj=io.BytesIO(archive)) as archive:
                archive.extractall(source, filter="data")
        else:
            names = subprocess.check_output(
                ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root
            ).decode().split("\0")
            for name in names:
                if name and (root / name).is_file():
                    destination = source / name
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(root / name, destination)
        (source / "Artifacts").symlink_to(artifacts, target_is_directory=True)
        config = source / "Sources/WireGuardApp/Config"
        (config / "Developer.xcconfig").write_text(
            "DEVELOPMENT_TEAM =\nAPP_ID_IOS = test.wireward.editor\nAPP_ID_MACOS = test.wireward.editor\n"
        )
        delegate = source / "Sources/WireGuardApp/UI/macOS/AppDelegate.swift"
        text = delegate.read_text()
        if text.count("@main\n") != 1:
            parser.error("Expected one @main entry point in the copied AppDelegate")
        harness = (root / "Tests/WireGuardAppTests/TunnelEditorRegression.swift").read_text()
        delegate.write_text(text.replace("@main\n", "", 1) + "\n" + harness)

        derived = output / "DerivedData"
        command = [
            "/usr/bin/xcodebuild", "-project", "WireGuard.xcodeproj", "-scheme", "WireGuardmacOS",
            "-configuration", "Debug", "-sdk", "macosx", "-destination", "generic/platform=macOS",
            "-derivedDataPath", str(derived), "CODE_SIGNING_ALLOWED=NO",
            "ARCHS=" + platform.machine(), "ONLY_ACTIVE_ARCH=YES", "build",
        ]
        with (output / "build.log").open("w") as log:
            built = subprocess.run(command, cwd=source, stdout=log, stderr=subprocess.STDOUT)
        if built.returncode:
            print("Editor test build failed; see", output / "build.log", flush=True)
            return built.returncode
        binary = derived / "Build/Products/Debug/WireGuard.app/Contents/MacOS/WireGuard"
        result = subprocess.run([str(binary)], cwd=source, env=os.environ.copy())
        return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
