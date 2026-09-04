# Tests for the node firmware, built as their own application.
#
# Separate from the firmware project because a test runner and a node are
# different programs: this one has no radio, no sockets and no outputs, and
# reaches app_main in two seconds so that a run is a run and not a wait.
cmake_minimum_required(VERSION 3.16)
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
project(componium_tests)
