#!/usr/bin/env python3
#
# Copyright (C) 2025 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
A user-friendly wrapper for the 'aninja' tool to inspect the Android build graph.

The Android Open Source Project (AOSP) uses the Soong build system, which generates
a 'build.ninja' file. The 'ninja' build tool then executes the build steps
defined in this file. 'aninja' is a tool provided with AOSP to query this
generated Ninja file, allowing developers to inspect dependencies, commands, and
the overall build graph.

This script simplifies common 'aninja' queries into easy-to-use modes. It is
designed to be invoked programmatically (e.g., by an LLM) or manually to help
diagnose build failures, understand artifact dependencies, and analyze how
specific files are built.
"""

import argparse
import os
import subprocess
import sys

# --- Helper Functions ---

def run_command(command, **kwargs):
    """
    Executes a shell command safely and returns its standard output.

    This function is a robust wrapper around Python's subprocess module. It
    captures output, checks for errors, and provides detailed error messages
    if the command fails. This is crucial for reliable execution in an
    automated environment.

    Args:
        command: A list of strings representing the command and its arguments
                 (e.g., ['ls', '-l']).
        **kwargs: Additional keyword arguments to pass to subprocess.run().

    Returns:
        The standard output of the command as a single, stripped string.

    Raises:
        SystemExit: If the command is not found or fails to execute, the script
                    will terminate with a non-zero exit code.
    """
    try:
        process = subprocess.run(
            command,
            capture_output=True,
            text=True,
            check=True,
            **kwargs
        )
        return process.stdout.strip()
    except FileNotFoundError:
        print(f"Error: Command '{command[0]}' not found. Is it in your PATH?", file=sys.stderr)
        sys.exit(1)
    except subprocess.CalledProcessError as e:
        print(f"Error executing command: {' '.join(command)}", file=sys.stderr)
        print(f"Return code: {e.returncode}", file=sys.stderr)
        print(f"Stdout:\n{e.stdout}", file=sys.stderr)
        print(f"Stderr:\n{e.stderr}", file=sys.stderr)
        sys.exit(1)

def get_product_config(variable_name):
    """
    Retrieves a product configuration variable from the Soong build system.

    AOSP builds are highly configurable via product variables like TARGET_PRODUCT
    and TARGET_DEVICE. This function provides a canonical way to fetch these
    values by invoking the build system's own variable dumping mechanism.

    Args:
        variable_name: The name of the variable to retrieve (e.g., 'TARGET_PRODUCT').

    Returns:
        The value of the specified configuration variable as a string.
    """
    soong_ui_path = 'build/soong/soong_ui.bash'
    if not os.path.exists(soong_ui_path):
        print(f"Error: '{soong_ui_path}' not found.", file=sys.stderr)
        print("Please run this script from the root of the AOSP source tree.", file=sys.stderr)
        sys.exit(1)

    return run_command([soong_ui_path, '--dumpvar-mode', variable_name])

# --- Sub-command Handlers ---

def handle_target_files_inputs(args):
    """
    Implements the 'target_files_inputs' mode.

    This function finds the list of inputs for the 'target-files.zip' package,
    which is a critical intermediate used for generating system images and OTA
    updates. It can operate in two modes: one for Soong-only inputs and one
    for the final, mixed-build system inputs.
    """
    print("🔎 Analyzing target files inputs...")
    target_path = ""
    if args.soong_only:
        # Soong-only mode: useful for debugging issues within Soong itself.
        product_name = get_product_config('TARGET_PRODUCT')
        search_dir = f'out/soong/.intermediates/build/soong/fsgen/{product_name}_generated_device'
        if not os.path.isdir(search_dir):
            print(f"Error: Directory not found: {search_dir}", file=sys.stderr)
            sys.exit(1)

        find_cmd = ['find', search_dir, '-name', 'target_files_dir.stamp']
        found_paths = run_command(find_cmd)
        if not found_paths:
            print(f"Error: 'target_files_dir.stamp' not found in {search_dir}", file=sys.stderr)
            sys.exit(1)
        target_path = found_paths.split('\n')[0]
        print(f"Found Soong-only target files stamp at: {target_path}")
    else:
        # Default mode: inspects the final artifact list from the mixed build.
        device = get_product_config('TARGET_DEVICE')
        product_name = get_product_config('TARGET_PRODUCT')
        target_path = (f'out/target/product/{device}/obj/PACKAGING/'
                       f'target_files_intermediates/{product_name}-target_files.zip.list')
        print(f"Using mixed build system target files list at: {target_path}")

    if not os.path.exists(target_path):
        print(f"Error: Target path does not exist: {target_path}", file=sys.stderr)
        print("Have you run a build first (e.g., 'm nothing')?", file=sys.stderr)
        sys.exit(1)

    aninja_cmd = ['aninja', '-t', 'query', target_path]
    result = run_command(aninja_cmd)
    print("\n--- aninja query result ---")
    print(result)
    print("--- end of result ---")


def handle_commands(args):
    """
    Implements the 'commands' mode.

    This function retrieves the command-line rule used to build a specified
    artifact. This is extremely useful for seeing the exact compiler flags,
    script arguments, or other options used to generate a file.
    """
    print(f"🔎 Getting last {args.n} build commands for target: {args.target}")
    if not os.path.exists(args.target):
        print(f"Error: Target path does not exist: {args.target}", file=sys.stderr)
        sys.exit(1)

    aninja_cmd = ['aninja', '-t', 'commands', args.target]
    full_output = run_command(aninja_cmd)
    lines = full_output.strip().split('\n')
    last_n_lines = lines[-args.n:]

    print(f"\n--- Last {len(last_n_lines)} of {len(lines)} commands ---")
    print('\n'.join(last_n_lines))
    print("--- end of result ---")


def handle_query(args):
    """
    Implements the 'query' mode.

    This function shows the direct inputs and outputs for a given build target.
    It helps answer the question: "What files are needed to build this target,
    and what files are produced by the same build rule?" This is fundamental for
    tracing build dependencies.
    """
    print(f"🔎 Querying inputs and outputs for target: {args.target}")
    if not os.path.exists(args.target):
        print(f"Error: Target path does not exist: {args.target}", file=sys.stderr)
        sys.exit(1)

    aninja_cmd = ['aninja', '-t', 'query', args.target]
    result = run_command(aninja_cmd)
    print("\n--- aninja query result ---")
    print(result)
    print("--- end of result ---")

# --- Main Execution ---

def main():
    """Parses command-line arguments and dispatches to the correct handler."""
    parser = argparse.ArgumentParser(
        description="A wrapper for 'aninja' to inspect AOSP build failures. Run from the root of your AOSP checkout.",
        formatter_class=argparse.RawTextHelpFormatter
    )
    subparsers = parser.add_subparsers(dest='command', required=True, help='Available commands')

    # 'target_files_inputs' sub-command
    parser_tfi = subparsers.add_parser(
        'target_files_inputs',
        help="Shows the full list of files that go into making the system image and OTA package.",
        description=(
            "ACTION: Inspects the inputs for the 'target-files.zip' archive.\n"
            "USE CASE: This is crucial for debugging packaging issues or understanding what is included in a final device build.\n"
            "By default, it shows the query output of the soong plus make build.\n"
            "The --soong-only flag shows the query output of the 'target-files.zip' of soong only build."
        )
    )
    parser_tfi.add_argument(
        '--soong-only',
        action='store_true',
        help='Inspect the soong only build dependency stamp instead of the final artifact list from the soong plus make build.'
    )
    parser_tfi.set_defaults(func=handle_target_files_inputs)

    # 'commands' sub-command
    parser_commands = subparsers.add_parser(
        'commands',
        help="Shows the exact command-line rule used to build a file.",
        description=(
            "ACTION: Prints the build rule (e.g., compiler or script call) that produces the specified target file.\n"
            "USE CASE: Use this to check the exact compiler flags, paths, and arguments used to build an artifact. Indispensable for debugging compilation errors."
        )
    )
    parser_commands.add_argument(
        '--target',
        required=True,
        help='The path to the target build artifact (e.g., out/soong/.../libc.so).'
    )
    parser_commands.add_argument(
        '-n',
        type=int,
        default=10,
        help='The number of last lines of the command to print. Some commands are long scripts (default: 10).'
    )
    parser_commands.set_defaults(func=handle_commands)

    # 'query' sub-command
    parser_query = subparsers.add_parser(
        'query',
        help="Shows the direct inputs and outputs for a specific build target.",
        description=(
            "ACTION: Shows the immediate inputs (dependencies) and outputs for a specific build target.\n"
            "USE CASE: This is the primary tool for dependency graph analysis. Use it to find out why a module is being rebuilt or to trace the source of a required file."
        )
    )
    parser_query.add_argument(
        '--target',
        required=True,
        help='The path to the target build artifact (e.g., out/soong/.../libc.so).'
    )
    parser_query.set_defaults(func=handle_query)

    args = parser.parse_args()
    args.func(args)

if __name__ == '__main__':
    main()