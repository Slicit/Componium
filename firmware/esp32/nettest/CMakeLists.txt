# The node, reachable, with no radio and no board.
#
# A separate project from the firmware because it has a different app_main: this
# one brings up the Ethernet controller QEMU emulates instead of the radio it
# does not. Everything below that line is the firmware itself, compiled from the
# same files the board runs.
cmake_minimum_required(VERSION 3.16)
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
project(componium_nettest)
