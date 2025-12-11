#!/usr/bin/env python3
"""Calculates the disk size of each repository managed by 'repo'.

This script invokes 'repo forall ...' to get the disk usage
for each project repository and prints the combined output as a CSV.
"""

import os
import re
import subprocess
import sys


def get_repo_disk_usage() -> dict[str, int]:
  """Invokes 'repo forall -p -c du -s .' and parses the output into a dictionary.

  Returns:
    A dictionary mapping project paths (str) to their disk size in bytes (int).
    Returns an empty dictionary if the input is empty or malformed.

  Raises:
      subprocess.CalledProcessError: If the command returns a non-zero exit
        code.
      FileNotFoundError: If the 'repo' command is not found.
  """
  output = subprocess.check_output(
      ["repo", "forall", "-p", "-c", "du", "-s", "-b", "."],
      text=True,
  )

  project_sizes: dict[str, int] = {}
  lines = output.strip().split("\n")
  current_project_name = None

  for line in lines:
    line = line.strip()
    if not line:
      continue  # Skip empty lines

    if line.startswith("project "):
      # Extract project name: remove "project " prefix and trailing "/"
      current_project_name = line.removeprefix("project ").removesuffix("/")
    elif current_project_name is not None:
      match = re.match(r"^(\d+)\s+\.$", line)
      if not match:
        continue
      size_str = match.group(1)
      project_sizes[current_project_name] = int(size_str)
      current_project_name = None  # Reset for the next project

  return project_sizes


def get_dot_repo_size() -> int:
  """Gets the disk usage of the '.repo' directory in bytes.

  Returns:
      The size of the '.repo' directory in bytes. Returns 0 if the command
      fails or the directory doesn't exist (du returns 0).

  Raises:
      FileNotFoundError: If the 'du' command is not found.
      # Note: subprocess.CalledProcessError is not explicitly raised on failure
      # because we want to return 0 in that case.
  """

  result = subprocess.check_output(["du", "-s", "-b", ".repo"], text=True)
  size_str = result.split()[0]
  return int(size_str)


def main():
  if not os.path.isdir(".repo"):
    sys.exit("Error: .repo directory not found, run inside a repo root.")

  project_sizes = get_repo_disk_usage()
  dot_repo_size = get_dot_repo_size()

  print("project_name,size_bytes")
  print(f".repo,{dot_repo_size}")
  for name, size_bytes in sorted(project_sizes.items()):
    print(f"{name},{size_bytes}")


if __name__ == "__main__":
  main()
