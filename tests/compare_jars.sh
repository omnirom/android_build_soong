#!/bin/bash

#
# Asserts that two JAR files are functionally identical by comparing the
# CRC-32 checksums of all their internal components.
#
# This function is designed for scripting and testing:
#  - If the JARs are identical, it prints NOTHING and returns an exit code of 0.
#  - If the JARs differ, it prints a single line describing the FIRST
#    point of difference to STDERR and returns an exit code of 1.
#
# DEPENDENCIES:
#   - unzip, awk, sort
#
assert_jars_equal() {
  # --- Input Validation ---
  if [[ "$#" -ne 2 ]]; then
    echo "Usage: assert_jars_equal <jar1_path> <jar2_path>" >&2
    return 1
  fi

  local jar1="$1"
  local jar2="$2"

  for cmd in unzip awk sort; do
    if ! command -v "$cmd" &>/dev/null; then
      echo "Error (assert_jars_equal): Required command '$cmd' not found." >&2
      return 1
    fi
  done

  for jar in "$jar1" "$jar2"; do
    if [[ ! -f "$jar" ]]; then
      echo "Error (assert_jars_equal): File not found: $jar" >&2
      return 1
    fi
    if ! unzip -tq "$jar" &>/dev/null; then
      echo "Error (assert_jars_equal): Not a valid JAR/ZIP file: $jar" >&2
      return 1
    fi
  done

  # --- Helper function to generate a manifest of "crc<space><space>filepath" ---
  _generate_manifest() {
    local jar_path="$1"
    unzip -v "$jar_path" 2>/dev/null | awk '
      /----/ {p=1-p; next}
      p {
        if (substr($NF, length($NF)) == "/") next;
        print $(NF-1) "  " $NF
      }
    ' | sort
  }

  # --- Core Logic using AWK ---
  # The awk script is designed to exit with a non-zero status code on the first
  # discrepancy it finds. If it processes everything without finding a
  # difference, it exits with a zero status code.
  # Output is redirected to stderr to keep stdout clean for success cases.
  awk '
    BEGIN {
        # Define colors for stderr output
        RED="\033[0;31m"; GREEN="\033[0;32m"; YELLOW="\033[0;33m";
        BOLD="\033[1m"; RESET="\033[0m";
    }

    # Pass 1: Load the manifest of the first JAR into a map.
    FNR==NR {
        path = substr($0, length($1) + 3);
        checksum_map[path] = $1;
        next;
    }

    # Pass 2: Process the second JARs manifest.
    {
        path = substr($0, length($1) + 3);
        checksum_new = $1;

        if (path in checksum_map) {
            # Path exists in both. If checksums differ, its a MODIFICATION.
            if (checksum_map[path] != checksum_new) {
                print BOLD YELLOW "DIFFERENCE (Modified):" RESET " " path > "/dev/stderr";
                exit 1; # Exit immediately with a failure code.
            }
            # If they match, delete from map. We use the remainder to find removed files.
            delete checksum_map[path];
        } else {
            # Path is new. This is an ADDITION.
            print BOLD GREEN "DIFFERENCE (Added):" RESET " " path > "/dev/stderr";
            exit 1; # Exit immediately with a failure code.
        }
    }

    # END Block: This code only runs if no "Modified" or "Added" files were found.
    # We now check if any files were removed.
    END {
        # The `for..in` loop will only run if the map is not empty.
        for (path in checksum_map) {
            # If we enter this loop, it means a file from jar1 was not in jar2.
            # This is a REMOVAL.
            print BOLD RED "DIFFERENCE (Removed):" RESET " " path > "/dev/stderr";
            exit 1; # Exit on the very first one we find.
        }
        # If the loop never runs, the map is empty, and the JARs are identical.
        # awk exits with a default code of 0 (success).
    }
  ' <(_generate_manifest "$jar1") <(_generate_manifest "$jar2")

  # Return the exit code of the awk command
  return $?
}