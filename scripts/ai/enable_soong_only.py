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

"""Automates the process of enabling Soong-only builds for Android products.

This script is repo-aware and handles the full workflow including branching,
testing, committing, uploading to Gerrit, and cleaning the workspace.
"""

import argparse
import logging
import os
import re
import subprocess

# --- Configuration ---
# AOSP root directory, typically set by the `envsetup.sh` script.
AOSP_ROOT = os.getenv("ANDROID_BUILD_TOP", ".")

# --- End Configuration ---

# Configure logging for clear output.
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)

def run_command(command, cwd=AOSP_ROOT, env=None, capture=False):
    """Executes a shell command and returns its result."""
    logging.info(f"Running command: `{' '.join(command)}` in `{cwd}`")
    try:
        result = subprocess.run(
            command,
            cwd=cwd,
            env=env,
            check=True,
            capture_output=capture,
            text=True,
        )
        return result
    except subprocess.CalledProcessError as e:
        logging.error(f"Command failed with exit code {e.returncode}.")
        if capture:
            logging.error(f"STDOUT:\n{e.stdout}")
            logging.error(f"STDERR:\n{e.stderr}")
        raise

def find_product_makefile(product_name):
    """Finds the primary makefile for a given product using the official script."""
    logging.info(f"Searching for makefile for product '{product_name}'...")
    script_path = os.path.join(AOSP_ROOT, "build/soong/scripts/find_product_mk_file.py")
    find_cmd = [script_path, "--name", product_name]

    try:
        result = run_command(find_cmd, capture=True)
        makefile_path = result.stdout.strip()
        if makefile_path:
            logging.info(f"✅ Found makefile: {makefile_path}")
            return makefile_path
        else:
            raise ValueError("find_product_mk_file.py returned an empty path.")
    except (subprocess.CalledProcessError, ValueError) as e:
        logging.error(f"❌ Could not find a .mk file for '{product_name}'. Error: {e}")
        return None


def run_soong_only_diff_test(product_name):
    """Runs the official `soong_only_diff_test.py` script."""
    logging.info(f"🚀 Starting Soong-only diff test for '{product_name}'...")
    script_path = os.path.join(AOSP_ROOT, "build/soong/scripts/soong_only_diff_test.py")
    test_cmd = [script_path, product_name]

    try:
        run_command(test_cmd)
        logging.info(f"✅ Verification PASSED for '{product_name}'. No differences found.")
        return True, None
    except subprocess.CalledProcessError as e:
        logging.warning(f"❌ Verification FAILED for '{product_name}'. Differences found.")
        return False, e.stderr

def enable_soong_only_in_makefile(product_name, makefile_path):
    """Modifies the product's makefile to add the Soong-only configuration."""
    logging.info(f"Adding Soong-only flag in '{makefile_path}'...")

    soong_only_block = (
        f"\n# Soong-only configuration for {product_name}\n"
        f"ifeq ($(TARGET_PRODUCT),{product_name})\n"
        f"PRODUCT_SOONG_ONLY := $(RELEASE_SOONG_ONLY_CUTTLEFISH)\n"
        f"endif\n"
    )

    full_path = os.path.join(AOSP_ROOT, makefile_path)
    with open(full_path, "r+") as f:
        content = f.read()
        if f"ifeq ($(TARGET_PRODUCT),{product_name})" in content and "PRODUCT_SOONG_ONLY" in content:
            logging.warning(f"Soong-only block appears to already exist in '{makefile_path}'. Skipping modification.")
            return

        f.seek(0, os.SEEK_END)
        f.write(soong_only_block)

    logging.info("Successfully modified makefile.")

def debug_and_fix_diffs_with_gemini(product_name, gemini_path, explain_mode=False):
    """Runs pre-checks, prepares context, and invokes the Gemini tool."""
    mode_string = "explain" if explain_mode else "fix"
    logging.warning(f"🤖 Soong-only diff test failed. Preparing to run Gemini in '{mode_string}' mode for '{product_name}'...")

    # 1. Run the pre-processing `pack` command (common for both modes).
    logging.info("Running pre-Gemini packaging command...")
    try:
        files_to_pack = [
            'build/soong/fsgen/artifact_path_requirements.go', 'build/soong/fsgen/boot_imgs.go',
            'build/soong/fsgen/config.go', 'build/soong/fsgen/filesystem_creator.go',
            'build/soong/fsgen/fsgen_mutators.go', 'build/soong/fsgen/prebuilt_etc_modules_gen.go',
            'build/soong/fsgen/super_img.go', 'build/soong/fsgen/util.go', 'build/soong/fsgen/vbmeta_partitions.go',
            'build/soong/android/packaging.go', 'build/soong/filesystem/aconfig_files.go',
            'build/soong/filesystem/android_device.go', 'build/soong/filesystem/android_device_product_out.go',
            'build/soong/filesystem/avb_add_hash_footer.go', 'build/soong/filesystem/avb_gen_vbmeta_image.go',
            'build/soong/filesystem/bootconfig.go', 'build/soong/filesystem/bootimg.go',
            'build/soong/filesystem/bootloader.go', 'build/soong/filesystem/boot_otas_16k.go',
            'build/soong/filesystem/check_partition_sizes.go', 'build/soong/filesystem/dtboimg.go',
            'build/soong/filesystem/filesystem.go', 'build/soong/filesystem/find_shareduid_violation_check.go',
            'build/soong/filesystem/fsverity_metadata.go', 'build/soong/filesystem/host_init_verifier_check.go',
            'build/soong/filesystem/logical_partition.go', 'build/soong/filesystem/prebuilt.go',
            'build/soong/filesystem/radio.go', 'build/soong/filesystem/ramdisk_16k.go',
            'build/soong/filesystem/raw_binary.go', 'build/soong/filesystem/recovery_background_pictures.go',
            'build/soong/filesystem/super_image.go', 'build/soong/filesystem/system_image.go',
            'build/soong/filesystem/system_other.go', 'build/soong/filesystem/vbmeta.go',
            'build/soong/android/variable.go', 'build/make/core/Makefile'
        ]
        pack_cmd = ['pack'] + files_to_pack
        run_command(pack_cmd)
        logging.info("✅ Pre-Gemini packaging successful.")
    except Exception as e:
        logging.error(f"❌ Failed to run the pre-Gemini `pack` command: {e}")
        raise

    # 2. Verify that the diff report from the test script actually exists.
    logging.info("Verifying that the diff report was generated...")
    report_path = os.path.join(AOSP_ROOT, "out/dist/soong_only_diffs", product_name, "comparison_report.txt")
    if not os.path.exists(report_path):
        logging.error(f"❌ Diff report not found at: {report_path}")
        logging.error("This indicates soong_only_diff_test.py may have crashed before completing.")
        raise FileNotFoundError(f"Diff report missing for product: {product_name}")
    logging.info("✅ Diff report found.")

    # 3. Select the correct instruction file based on the mode.
    if explain_mode:
        instructions_filename = "ai/GEMINI_EXPLAIN_INSTRUCTIONS.md"
    else:
        instructions_filename = "ai/GEMINI_DEBUG_INSTRUCTIONS.md"

    instructions_path = os.path.join(AOSP_ROOT, "build/soong/scripts", instructions_filename)
    logging.info(f"Loading Gemini instructions from '{instructions_path}'...")
    try:
        with open(instructions_path, 'r') as f:
            prompt_content = f.read()
    except FileNotFoundError:
        logging.error(f"FATAL: Gemini instructions file not found at '{instructions_path}'. Cannot proceed.")
        raise

    # Append the product-specific context.
    additional_context = (
        f"\n\n* **Product Name:** {product_name}\n"
        f"* **Initial Diff Report Path:** @out/dist/soong_only_diffs/{product_name}/comparison_report.txt"
    )
    prompt_content += additional_context
    logging.info("Appended dynamic product context to Gemini prompt.")

    # 4. Invoke the Gemini CLI tool using the provided path.
    gemini_cmd = [gemini_path, "--yolo", "-p", prompt_content]
    try:
        run_command(gemini_cmd)
        logging.info("Gemini tool executed successfully.")
    except Exception as e:
        logging.error(f"Error during Gemini execution: {e}")
        raise

def commit_changes_in_projects(branch_name, commit_message):
    """Finds modified projects, starts a branch in them, and commits the changes."""
    logging.info("Checking for modified projects...")

    status_output = run_command(['repo', 'status'], capture=True).stdout

    project_paths = re.findall(r'^project\s+([^\s]+)', status_output, re.MULTILINE)

    if not project_paths:
        logging.warning("No changes detected in any project. Nothing to commit.")
        return

    logging.info(f"Found changes in the following projects: {', '.join(project_paths)}")

    for path in project_paths:
        full_project_path = os.path.join(AOSP_ROOT, path.strip())
        logging.info(f"--- Processing project '{path}' ---")
        try:
            logging.info(f"Starting branch '{branch_name}'...")
            run_command(['repo', 'start', branch_name, '.'], cwd=full_project_path)
            run_command(['git', 'add', '-A'], cwd=full_project_path)
            run_command(['git', 'commit', '-m', commit_message], cwd=full_project_path)
            run_command(["repo", "upload", ".", "--yes", "-c", "-t", "--no-verify"], cwd=full_project_path)
            logging.info(f"✅ Successfully uploaded branch '{branch_name}' in '{full_project_path}'.")
        except Exception as e:
            logging.error(f"Failed to commit and upload changes in '{path}': {e}")

    logging.info("Finished processing all modified projects.")

def clean_workspace(head_branch):
    """Resets all repos and applies a specific local change in build/soong."""
    logging.info("--- 🧹 Cleaning workspace for next iteration ---")

    # 1. Reset all repositories to a clean state, checking out the specified head branch.
    cleanup_cmd_str = f"git reset -q --hard HEAD && git clean -q -fdx && git checkout -q {head_branch} || true"

    cleanup_cmd = ["repo", "forall", "-q", "-e", "-c", cleanup_cmd_str]
    try:
        run_command(cleanup_cmd)
        logging.info("✅ Workspace cleaned successfully.")
    except Exception as e:
        logging.error(f"CRITICAL: Failed to clean the workspace: {e}")
        raise SystemExit("Workspace cleanup failed.")


def main():
    """Main function to parse arguments and orchestrate the workflow."""
    if not AOSP_ROOT:
        raise EnvironmentError("AOSP environment not set up. Please run `source build/envsetup.sh`.")

    parser = argparse.ArgumentParser(description="Automate Soong-only enablement for AOSP products.")
    parser.add_argument(
        "products",
        metavar="PRODUCT_NAME",
        type=str,
        nargs='+',
        help="A list of product names to process (e.g., aosp_arm aosp_x86_64)."
    )
    parser.add_argument(
        "--head-branch",
        type=str,
        required=True,
        help="Required: The name of the main branch to check out and rebase onto during cleanup."
    )
    parser.add_argument(
        "--gemini-path",
        type=str,
        required=True,
        help="Required: The absolute path to the Gemini CLI executable."
    )
    parser.add_argument(
        "--explain",
        action="store_true",
        help="Run in explain mode. This will generate a report from Gemini without making code changes or cleaning the workspace."
    )
    args = parser.parse_args()

    for product in args.products:
        logging.info(f"--- Starting processing for product: {product} ---")
        branch_name = f"soong_only_{product}"

        try:
            passed, _ = run_soong_only_diff_test(product)
            commit_msg = ""

            if not passed:
                debug_and_fix_diffs_with_gemini(product, args.gemini_path, args.explain)
                if not args.explain:
                    # Only set a commit message if we are NOT in explain mode.
                    commit_msg = (
                        f"fix: Fix Soong-only diffs for {product}\n\n"
                        "Applied automated fixes generated by the Gemini tool."
                    )
            elif not args.explain:
                # Test passed AND we are in the standard "fix" mode.
                makefile = find_product_makefile(product)
                if makefile:
                    enable_soong_only_in_makefile(product, makefile)
                    commit_msg = \
f"""Enable soong only build for {product}

Flag: build.RELEASE_SOONG_ONLY_CUTTLEFISH
Bug: 427983604
Test: build/soong/scripts/soong_only_diff_test.py {product}
"""
            else:
                # Test passed AND we are in "explain" mode.
                logging.info(f"✅ No diffs found for '{product}'. Nothing to explain.")

            # This block only runs if a commit_msg was generated (i.e., not in explain mode).
            if commit_msg:
                commit_changes_in_projects(branch_name, commit_msg)

            logging.info(f"✅ Successfully processed '{product}'.")

        except Exception as e:
            logging.error(f"An unexpected error occurred while processing '{product}': {e}", exc_info=False)
            logging.error(f"Skipping to cleanup for '{product}'.")

        finally:
            if not args.explain:
                clean_workspace(args.head_branch)

    logging.info("--- All products processed. ---")

if __name__ == "__main__":
    main()