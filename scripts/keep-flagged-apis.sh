#!/bin/bash -e
#
# Copyright 2023 Google Inc. All rights reserved.
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

# Convert a list of flags in the input file to a list of metalava options
# that will keep the APIs for those flags will hiding all other flagged
# APIs.

FLAGS="$1"

FLAGGED="android.annotation.FlaggedApi"

# Convert the list of feature flags in the input file to Metalava options
# of the form `--revert-annotation !android.annotation.FlaggedApi("<flag>")`
# to prevent the annotated APIs from being hidden, i.e. include the annotated
# APIs in the SDK snapshots.
while read -r line; do
  # Escape and quote the key for sed
  escaped_line=$(echo "$line" | sed "s/'/\\\'/g; s/ /\\ /g")

  echo "--revert-annotation '!$FLAGGED(\"$escaped_line\")'"
done < "$FLAGS"

# Revert all flagged APIs, unless listed above.
echo "--revert-annotation $FLAGGED"
