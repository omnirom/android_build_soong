#!/bin/bash -ex

# Copyright 2017 Google Inc. All rights reserved.
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

if [ -z "${OUT_DIR}" ]; then
    echo Must set OUT_DIR
    exit 1
fi

# Note: NDK doesn't support flagging APIs, so we hardcode it to trunk_staging.
# TODO: remove ALLOW_MISSING_DEPENDENCIES=true when all the riscv64
# dependencies exist (currently blocked by http://b/273792258).
# TODO: remove BUILD_BROKEN_DISABLE_BAZEL=1 when bazel supports riscv64 (http://b/262192655).
#
# LTO is disabled because the NDK compiler is not necessarily in-sync with the
# compiler used to build the platform sysroot, and the sysroot includes static
# libraries which would be incompatible with mismatched compilers when built
# with LTO. Disabling LTO globally for the NDK sysroot is okay because the only
# compiled code in the sysroot that will end up in apps is those static
# libraries.
# https://github.com/android/ndk/issues/1591
TARGET_RELEASE=trunk_staging \
ALLOW_MISSING_DEPENDENCIES=true \
BUILD_BROKEN_DISABLE_BAZEL=1 \
DISABLE_LTO=true \
    TARGET_PRODUCT=ndk build/soong/soong_ui.bash --make-mode --soong-only ${OUT_DIR}/soong/ndk.timestamp

if [ -n "${DIST_DIR}" ]; then
    mkdir -p ${DIST_DIR} || true
    tar cjf ${DIST_DIR}/ndk_platform.tar.bz2 -C ${OUT_DIR}/soong ndk
fi
