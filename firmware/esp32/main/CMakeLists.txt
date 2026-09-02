idf_component_register(SRCS "componium_node.c" "app_main.c" "wifi.c" "improv.c" "led.c" "sacn.c" "devices.c" "config.c"
                       INCLUDE_DIRS "."
                       REQUIRES driver esp_timer json lwip mbedtls esp_wifi esp_netif nvs_flash esp_event app_update)
