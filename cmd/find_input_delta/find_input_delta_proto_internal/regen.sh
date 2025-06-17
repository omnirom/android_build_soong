#!/bin/bash

aprotoc --go_out=paths=source_relative:.  internal_state.proto
