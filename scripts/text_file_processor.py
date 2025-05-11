#!/usr/bin/env python
#
# Copyright (C) 2024 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the 'License');
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an 'AS IS' BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import argparse
import re

def main():
    parser = argparse.ArgumentParser(description='This script looks for '
        '`{CONTENTS_OF:path/to/file}` markers in the input file and replaces them with the actual '
        'contents of that file, with leading/trailing whitespace stripped. The idea is that this '
        'script could be extended to support more types of markers in the future.')
    parser.add_argument('input')
    parser.add_argument('output')
    args = parser.parse_args()

    with open(args.input, 'r') as f:
        contents = f.read()

    i = 0
    replacedContents = ''
    for m in re.finditer(r'{CONTENTS_OF:([a-zA-Z0-9 _/.-]+)}', contents):
        replacedContents += contents[i:m.start()]
        with open(m.group(1), 'r') as f:
            replacedContents += f.read().strip()
        i = m.end()
    replacedContents += contents[i:]

    with open(args.output, 'w') as f:
        f.write(replacedContents)


if __name__ == '__main__':
    main()
