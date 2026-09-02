idf_component_register(SRCS "componium_node.c" "app_main.c" "wifi.c" "improv.c" "led.c" "sacn.c" "devices.c" "config.c"
                       INCLUDE_DIRS "."
                       REQUIRES driver esp_timer json lwip mbedtls esp_wifi esp_netif nvs_flash esp_event app_update)

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
