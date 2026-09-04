# The firmware's own sources, compiled again here rather than copied.
#
# By path, so that there is exactly one config.c in the world and these tests
# are about it. Only the files that parse and decide are listed: the ones that
# drive hardware need peripherals QEMU does not have, and testing a stub of the
# chip would prove things about the stub.
idf_component_register(
    SRCS "test_main.c"
         "../../main/guard.c"
         "../../main/config.c"
         "../../main/devices.c"
    INCLUDE_DIRS "." "../../main"
    REQUIRES unity json nvs_flash driver esp_timer)
