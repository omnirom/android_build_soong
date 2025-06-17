#!/bin/bash

aprotoc --go_out=paths=source_relative:. file_list.proto
