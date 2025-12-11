#!/usr/bin/env python3
#
# Copyright (C) 2024 The Android Open Source Project
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
#

"""
This script is used as a replacement for the Rust linker to allow fine-grained
control over the what gets emitted to the linker.
"""

import os
import shutil
import subprocess
import sys
import argparse

replacementVersionScript = None

argparser = argparse.ArgumentParser()
argparser.add_argument('--android-clang-bin', required=True)
args = argparser.parse_known_args()
old_clang_args = [args[0].android_clang_bin] + args[1]
new_clang_args = list()

# Add/remove args
for i, arg in enumerate(old_clang_args):
   # Record and remove the custom android-version-script arg
   if arg.startswith('-Wl,--android-version-script='):
      replacementVersionScript = arg.split("=")[1]
      continue

   # Remove object files rustc emits for Windows target
   # We provide these as an rlib.
   if arg == "rsend.o":
      continue
   if arg == "rsbegin.o":
      continue

   # Keep the arg
   new_clang_args.append(old_clang_args[i])

# Modify args
if replacementVersionScript:
   versionScriptFound = False
   for i, arg in enumerate(new_clang_args):
      if arg.startswith('-Wl,--version-script='):
         new_clang_args[i] ='-Wl,--version-script=' + replacementVersionScript
         versionScriptFound = True
         break

   if not versionScriptFound:
       # If rustc did not emit a version script, just append the arg
       new_clang_args.append('-Wl,--version-script=' + replacementVersionScript)
try:
   subprocess.run(new_clang_args, encoding='utf-8', check=True)
except subprocess.CalledProcessError as e:
   sys.exit(-1)

