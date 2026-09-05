# The firmware's own sources, by path, so there is one componium_node.c in the
# world and this is a test of it. wifi.c and improv.c are left out: they are the
# two things QEMU cannot run, and web.c is left out because it asks wifi.c where
# it is.
idf_component_register(
    SRCS "net_main.c"
         "../../main/componium_node.c"
         "../../main/devices.c"
         "../../main/config.c"
         "../../main/guard.c"
         "../../main/led.c"
         "../../main/sacn.c"
         "../../main/web.c"
    INCLUDE_DIRS "." "../../main"
    REQUIRES driver esp_timer json lwip mbedtls esp_netif nvs_flash esp_event
             esp_eth app_update esp_http_server)

# The shared secret comes from the environment, so that building a real board
# does not mean editing a tracked file:
#
#   COMPONIUM_CIP_SECRET='...' idf.py build
#
# Unset builds a board with no secret, which accepts no configuration at all.
# That is the right default and a poor board: it cannot be told what is wired to
# it, so it is only useful as a light that speaks sACN.
if(DEFINED ENV{COMPONIUM_CIP_SECRET})
    target_compile_definitions(${COMPONENT_LIB} PRIVATE
                               CIP_SECRET="$ENV{COMPONIUM_CIP_SECRET}")
    message(STATUS "componium: building with a shared secret")
else()
    message(WARNING
        "componium: no COMPONIUM_CIP_SECRET, so this board will refuse all configuration")
endif()
