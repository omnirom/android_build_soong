#!/usr/bin/env python3
#
# Copyright (C) 2018 The Android Open Source Project
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

"""This file checks baselines passed to Android Lint for checks that must not be baselined."""

import argparse
import sys
from xml.dom import minidom

from ninja_rsp import NinjaRspFileReader


def parse_args():
  """Parse commandline arguments."""

  def convert_arg_line_to_args(arg_line):
    for arg in arg_line.split():
      if arg.startswith('#'):
        return
      if not arg.strip():
        continue
      yield arg

  parser = argparse.ArgumentParser(fromfile_prefix_chars='@')
  parser.convert_arg_line_to_args = convert_arg_line_to_args
  parser.add_argument('--name', dest='name',
                      help='name of the module.')
  parser.add_argument('--baselines', dest='baselines', action='append', default=[],
                      help='file containing whitespace separated list of baseline files.')
  parser.add_argument('--disallowed_issues', dest='disallowed_issues', default=[],
                     help='lint issues disallowed in the baseline file')
  return parser.parse_args()


def check_baseline_for_disallowed_issues(baseline, forced_checks):
  issues_element = baseline.documentElement
  if issues_element.tagName != 'issues':
    raise RuntimeError('expected issues tag at root')
  issues = issues_element.getElementsByTagName('issue')
  disallowed = set()
  for issue in issues:
    id = issue.getAttribute('id')
    if id in forced_checks:
      disallowed.add(id)
  return disallowed


def main():
  """Program entry point."""
  args = parse_args()

  error = False
  for baseline_rsp_file in args.baselines:
    for baseline_path in NinjaRspFileReader(baseline_rsp_file):
      baseline = minidom.parse(baseline_path)
      disallowed_issues = check_baseline_for_disallowed_issues(baseline, args.disallowed_issues)
      if disallowed_issues:
        print('disallowed issues %s found in lint baseline file %s for module %s'
                % (disallowed_issues, baseline_path, args.name))
        error = True

  if error:
    sys.exit(1)


if __name__ == '__main__':
  main()
