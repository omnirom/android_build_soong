from argparse import ArgumentParser
import json
import os
from pathlib import Path
import subprocess
import sys
from typing import Dict, List


def source_to_targets(  ANDROID_BUILD_TOP: Path, source_file: Path):
  """Get list of Soong targets for a pathfile"""
  target = []
  MAPPING_PATH = (
      ANDROID_BUILD_TOP / "out" / "soong" / "rust-target-mapping.json"
  )
  try:
    with open(MAPPING_PATH, "r") as file:
      for entry in json.load(file):
        if str(source_file).startswith(entry["source_dir"]):
          check = entry["check_target"]
          product = entry["TARGET_PRODUCT"]
          release= entry["TARGET_RELEASE"]
          build_variant= entry["TARGET_BUILD_VARIANT"]
          target.append((check, product, release, build_variant))
    return target
  except FileNotFoundError:
    raise Exception(
        'The mapping file is not here, please run "SOONG_GEN_RUST_PROJECT=1'
        f' SOONG_LINK_RUST_PROJECT_TO={ANDROID_BUILD_TOP} m nothing"'
    )


def run_targets(targets):
  """Build all the Soong output targets"""
  soong_ui_path = ANDROID_BUILD_TOP / "build" / "soong" / "soong_ui.bash"
  soong_ui_cmd = [
      str(soong_ui_path),
      # This will build using the target(s) name.
      "--make-mode",
      # This will skip the kati, kati ninja and ninja build steps
      "--soong-only",
  ]
  for (check, product, release, build_variant) in targets:
    cmd = soong_ui_cmd + [str(check)]
    env= {
          "TARGET_PRODUCT": product,
          "TARGET_RELEASE": release,
          "TARGET_BUILD_VARIANT": build_variant,
      }
    result = subprocess.run(
        cmd,
        env=env,
        stdout=sys.stderr,
        stderr=sys.stderr,
    )
    if result.returncode < 0:
      raise Exception(f"Command failed. Killed by signal {result.returncode}")


def gather_output(ANDROID_BUILD_TOP: Path, check_targets):
  """Read out the generated output files and print results"""
  # We use the error flag when it expects the message flag to be used
  #  so we need to augment the result by adding the reason field to the JSON
  diagnostics = set()
  for (clippy_error_file,_,_,_) in check_targets:
    path = str(ANDROID_BUILD_TOP) + "/" + clippy_error_file + ".error"
    try:
      with open(path, "r") as file:
        for line in file:
          diagnostics.add(line.strip())
    except FileNotFoundError:
      continue
  for diagnostic in diagnostics:
    print(diagnostic)
    print(diagnostic, file=sys.stderr)


parser = ArgumentParser(
    description="Rust-analyzer integration for rustc/clippy-driver binary"
)
parser.add_argument(
    "source_file",
    type=Path,
    help="The absolute path of the Rust source file to run this check on",
)
args = parser.parse_args()
exe_path = Path(__file__).resolve()
ANDROID_BUILD_TOP = exe_path.parents[3]
source_file = args.source_file.relative_to(ANDROID_BUILD_TOP)
targets = source_to_targets(ANDROID_BUILD_TOP, source_file)
run_targets(targets)
gather_output(ANDROID_BUILD_TOP, targets)

