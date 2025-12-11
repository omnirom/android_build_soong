#!/bin/bash
#
# Copyright (C) 2025 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# A wrapper around rustc/clippy to set up the correct absolute path for OUT_DIR.
# This has to be done on invocation rather than precalucalted in Soong to
# ensure that RBE calculates its own absolute path rather than inheriting the
# local host absolute path.
#
# The first argument should be the path to rustc/clippy
set -e

# Check if $SOONG_RUST_GEN_DIR is set, otherwise don't do anything.
if [ ! -z "${SOONG_RUST_GEN_DIR}" ]; then
  if [[ $SOONG_RUST_GEN_DIR =~ ^/ ]]; then
    export OUT_DIR=$SOONG_RUST_GEN_DIR
  else
    export OUT_DIR=`pwd`/$SOONG_RUST_GEN_DIR
  fi
fi

exec_path=$1
shift
$exec_path "$@"
# Loop through arguments and determine if we emitted a dep file.
# If so do some post-processing on it.
while [[ "$#" -gt 0 ]]; do
    case "$1" in
        --emit)
            # Split the emit argument and check that it's a dep-info type
            IFS='=' read -r -a emit_args <<< "$2"
            if [[ ${emit_args[0]} == "dep-info" ]]; then
                # strip off the .raw extension as the deps file is expected to be
                raw_deps=${emit_args[1]}
                deps_file=${raw_deps%.*}
                out_file=${deps_file%.*}

                #Rustc deps-info writes out make compatible dep files: https://github.com/rust-lang/rust/issues/7633
                #Rustc emits unneeded dependency lines for the .d and input .rs files.
                #Those extra lines cause ninja warning:
                #    "warning: depfile has multiple output paths"
                #For ninja, we keep/grep only the dependency rule for the rust $out file.
                grep ^$out_file: $raw_deps > $deps_file

                # Absolute paths from include! macros (and similar) can cause a mismatch between
                # RBE and local dep info files, so strip out $ANDROID_BUILD_TOP
                sed -i -e "s|`pwd`/||g" $deps_file
                break
            else
              shift 2
            fi
            ;;
    esac
    shift
done
