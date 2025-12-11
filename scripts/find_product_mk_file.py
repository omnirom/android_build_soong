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
A utility to find the product makefile that defines a specific PRODUCT_NAME
"""

import os
import argparse
import subprocess

def find_product_file(product_name: str):
    env = os.environ
    env['TARGET_PRODUCT'] = product_name
    env['TARGET_RELEASE'] = 'trunk_staging'

    subprocess.run([
        'build/soong/soong_ui.bash',
        '--dumpvar-mode',
        'PRODUCT_MAKEFILE_PATH',
    ], env=env)

def main():
    """Main function to parse arguments and drive the script."""
    parser = argparse.ArgumentParser(
        description="Find a product's .mk definition",
        formatter_class=argparse.RawTextHelpFormatter,
        epilog="""
Example Usage:
  # Navigate to your root directory and run the script
  python3 find_product_mk.py --name aosp_cf_x86_64_phone
"""
    )
    parser.add_argument(
        '--name',
        required=True,
        help="The exact product name to search for (e.g., 'aosp_arm64')."
    )

    args = parser.parse_args()
    product_name = args.name

    find_product_file(product_name)

if __name__ == "__main__":
    main()
