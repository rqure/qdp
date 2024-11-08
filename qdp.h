#pragma once


#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdio.h>

#define QDP_MAX_BUFFER_SIZE 10024
#define QDP_MAX_DEVICES 64

// Enum for payload types
typedef enum {    
    // TELEMETRY_EVENT
    // Reports updated telemetry data when a device condition changes.
    // Example: Temperature reading of 25.5°C as a float.
    // TELEMETRY_EVENT PAYLOAD := [F] [4] [0x41, 0xC8, 0x00, 0x00]
    PAYLOAD_TYPE_TELEMETRY_EVENT = 0x01,

    // TELEMETRY_REQUEST
    // Requests current telemetry data from a device. Payload is empty.
    // TELEMETRY_REQUEST PAYLOAD := [N] [0] []
    PAYLOAD_TYPE_TELEMETRY_REQUEST,

    // TELEMETRY_RESPONSE
    // Responds to TELEMETRY_REQUEST with requested data.
    // TELEMETRY_RESPONSE PAYLOAD := [UI] [4] [0x00, 0x00, 0x00, 0x41]
    PAYLOAD_TYPE_TELEMETRY_RESPONSE,

    // COMMAND_REQUEST
    // Sends a command to the device with optional parameters.
    // Example: Command to set temperature threshold to 22.5°C.
    // COMMAND_REQUEST PAYLOAD := [F] [4] [0x41, 0x38, 0x00, 0x00]
    PAYLOAD_TYPE_COMMAND_REQUEST,

    // COMMAND_RESPONSE
    // Confirms execution of COMMAND_REQUEST with success/failure and result.
    // COMMAND_RESPONSE PAYLOAD := [F] [4] [0x41, 0x38, 0x00, 0x00]
    PAYLOAD_TYPE_COMMAND_RESPONSE,

    // DEVICE_ID_REQUEST
    // Requests a list of device IDs managed by a network device. Payload is empty.
    // DEVICE_ID_REQUEST PAYLOAD := [N] [0] []
    PAYLOAD_TYPE_DEVICE_ID_REQUEST,

    // DEVICE_ID_RESPONSE
    // Responds to DEVICE_ID_REQUEST with a list of device IDs.
    // DEVICE_ID_RESPONSE PAYLOAD := [A] [24] [UI] [4] [0x00, 0x00, 0x00, 0x01] [UI] [4] [0x00, 0x00, 0x00, 0x02]
    PAYLOAD_TYPE_DEVICE_ID_RESPONSE,

    // ERROR_RESPONSE
    // Reports failure of a request with error details.
    // ERROR_RESPONSE PAYLOAD := [S] [13] [0x45, 0x72, 0x72, 0x6F, 0x72, 0x20, 0x4D, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65]
    PAYLOAD_TYPE_ERROR_RESPONSE
} QDPPayloadType;

// Enum for data types in the payload
typedef enum {
    DATA_TYPE_NULL,       // No data
    DATA_TYPE_INT,        // 32-bit signed integer
    DATA_TYPE_UINT,       // 32-bit unsigned integer
    DATA_TYPE_FLOAT,      // 32-bit floating-point
    DATA_TYPE_STRING,     // Variable-length string
    DATA_TYPE_ARRAY       // Array of data
} QDPDataType;

// Struct for data contained in the payload
typedef struct {
    uint32_t data_type;       // Type of the data
    uint32_t size;          // Size of the content
    void *content;          // Pointer to the content (can be of any data_type)
} QDPPayload;

// Header struct
typedef struct {
    uint32_t from;             // Source device ID
    uint32_t to;               // Target device ID (0 for broadcast)
    uint32_t payload_type;     // Type of payload
    uint32_t correlation_id;   // Correlation ID for tracking responses
} QDPHeader;

// Message struct
typedef struct {
    uint8_t *buffer;          // Pointer to the message buffer
    QDPHeader *header;        // Pointer to dynamically allocated header
    QDPPayload *payload;      // Pointer to dynamically allocated payload
    uint32_t* checksum;        // Checksum/CRC for data integrity
} QDPMessage;

typedef struct {
    bool (*fn) (QDPMessage *, void *);
    void *ctx;
} QDPCallback;

typedef struct {
    uint32_t device_id;
    QDPCallback data_change_callback;
} QDPDevice;

typedef struct {
    QDPCallback send;
    QDPCallback recv;
    
    QDPCallback on_command_request;
    QDPCallback on_event_or_response;
    
    QDPDevice devices[QDP_MAX_DEVICES];
    uint32_t total_devices;

    uint8_t rx_buffer[QDP_MAX_BUFFER_SIZE];
    uint8_t tx_buffer[QDP_MAX_BUFFER_SIZE];
} QDPHandle;

QDPMessage qdp_message_from_buffer(uint8_t *buffer) {
    QDPMessage msg;

    msg.buffer = buffer;
    msg.header = (QDPHeader *) buffer;
    
    msg.payload = (QDPPayload *) (buffer + sizeof(QDPHeader));
    msg.payload->content = (void *) (buffer + sizeof(QDPHeader) + sizeof(QDPPayload) - sizeof(void *));

    msg.checksum = (uint32_t *) (msg.payload->content + msg.payload->size);

    return msg;
}

int32_t qdp_payload_get_int(QDPPayload *payload) {
    if (payload == NULL || payload->data_type != DATA_TYPE_INT) {
        return 0;
    }

    return *((int32_t *) payload->content);
}

uint32_t qdp_payload_get_uint(QDPPayload *payload) {
    if (payload == NULL || payload->data_type != DATA_TYPE_UINT) {
        return 0;
    }

    return *((uint32_t *) payload->content);
}

float qdp_payload_get_float(QDPPayload *payload) {
    if (payload == NULL || payload->data_type != DATA_TYPE_FLOAT) {
        return 0.0;
    }

    return *((float *) payload->content);
}

char *qdp_payload_get_string(QDPPayload *payload) {
    if (payload == NULL || payload->data_type != DATA_TYPE_STRING) {
        return NULL;
    }

    return (char *) payload->content;
}

QDPPayload *qdp_payload_get_array_index(QDPPayload *payload, uint32_t index) {
    if (payload->data_type != DATA_TYPE_ARRAY) {
        return NULL;
    }
    
    uint32_t offset = 0;
    for (uint32_t i = 0; i < index; i++) {
        if (offset >= payload->size) {
            return NULL;
        }

        QDPPayload* payload_i = (QDPPayload *) (payload->content + offset);
        offset += sizeof(QDPPayload) - sizeof(void *) + payload_i->size;
    }

    return (QDPPayload *) (payload->content + offset);
}

void qdp_payload_set_null(QDPPayload *payload) {
    payload->data_type = DATA_TYPE_NULL;
    payload->size = 0;
}

void qdp_payload_reset(QDPPayload *payload) {
    qdp_payload_set_null(payload);    
}

void qdp_payload_clear_array(QDPPayload *payload) {
    payload->data_type = DATA_TYPE_ARRAY;
    payload->size = 0;
}

void qdp_payload_set_int(QDPPayload *payload, int32_t value) {
    payload->data_type = DATA_TYPE_INT;
    payload->size = sizeof(int32_t);
    memcpy(payload->content, &value, payload->size);
}

void qdp_payload_set_uint(QDPPayload *payload, uint32_t value) {
    payload->data_type = DATA_TYPE_UINT;
    payload->size = sizeof(uint32_t);
    memcpy(payload->content, &value, payload->size);
}

void qdp_payload_set_float(QDPPayload *payload, float value) {
    payload->data_type = DATA_TYPE_FLOAT;
    payload->size = sizeof(float);
    memcpy(payload->content, &value, payload->size);
}

void qdp_payload_set_string(QDPPayload *payload, char *value) {
    payload->data_type = DATA_TYPE_STRING;
    payload->size = strlen(value) + 1;
    memcpy(payload->content, value, payload->size);
}

void qdp_payload_set_array(QDPPayload *payload, QDPPayload *array, uint32_t count) {
    payload->data_type = DATA_TYPE_ARRAY;
    payload->size = 0;
    for (uint32_t i = 0; i < count; i++) {
        QDPPayload *payload_i = (QDPPayload *) (payload->content + payload->size);
        payload_i->data_type = array[i].data_type;
        payload_i->size = array[i].size;
        memcpy(payload_i->content, array[i].content, array[i].size);
        payload->size += sizeof(QDPPayload) - sizeof(void *) + array[i].size;
    }
}

void qdp_payload_append_int(QDPPayload *payload, int32_t value) {
    QDPPayload *new_payload = (QDPPayload *) (payload->content + payload->size);
    qdp_payload_set_int(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}

void qdp_payload_append_uint(QDPPayload *payload, uint32_t value) {
    QDPPayload *new_payload = (QDPPayload *) (payload->content + payload->size);
    qdp_payload_set_uint(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}

void qdp_payload_append_float(QDPPayload *payload, float value) {
    QDPPayload *new_payload = (QDPPayload *) (payload->content + payload->size);
    qdp_payload_set_float(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}

void qdp_payload_append_string(QDPPayload *payload, char *value) {
    QDPPayload *new_payload = (QDPPayload *) (payload->content + payload->size);
    qdp_payload_set_string(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}


// To maintain data integrity, QDP uses a 32-bit CRC (Cyclic Redundancy Check), calculated over the HEADER and PAYLOAD sections.
// The protocol uses CRC32 with polynomial 0xEDB88320.
// CRC Calculation
//     Initialize the CRC table using the polynomial.
//     Process each byte in [HEADER] [PAYLOAD] using the CRC table.
//     XOR the final CRC with 0xFFFFFFFF.
// CRC Verification
//     Extract received CRC.
//     Calculate CRC over [HEADER] [PAYLOAD].
//     Verify that calculated CRC matches the received CRC.

#define QDP_POLYNOMIAL 0xEDB88320
uint32_t qdp_crc32_table[256];

void qdp_generate_crc32_table() {
    for (int i = 0; i < 256; i++) {
        uint32_t crc = i;
        for (int j = 0; j < 8; j++) {
            crc = (crc & 1) ? (crc >> 1) ^ QDP_POLYNOMIAL : crc >> 1;
        }
        qdp_crc32_table[i] = crc;
    }
}

uint32_t qdp_calculate_crc32(const QDPMessage *msg) {
    uint8_t *data = msg->buffer;
    uint32_t length = sizeof(QDPHeader) + sizeof(QDPPayload) - sizeof(void *) + msg->payload->size;

    uint32_t crc = 0xFFFFFFFF;
    for (size_t i = 0; i < length; i++) {
        crc = (crc >> 8) ^ qdp_crc32_table[(crc ^ data[i]) & 0xFF];
    }

    return crc ^ 0xFFFFFFFF;
}

void qdp_update_crc32(const QDPMessage *msg) {
    *msg->checksum = qdp_calculate_crc32(msg);
}

bool qdp_verify_crc32(const QDPMessage *msg) {
    return *msg->checksum == qdp_calculate_crc32(msg);
}

void qdp_swap_from_to(QDPMessage *msg) {
    uint32_t temp = msg->header->from;
    msg->header->from = msg->header->to;
    msg->header->to = temp;
}

void qdp_register_device(QDPHandle *handle, uint32_t device_id, QDPCallback data_change_callback) {
    QDPDevice *device = &handle->devices[handle->total_devices];
    device->device_id = device_id;
    device->data_change_callback = data_change_callback;
    handle->total_devices++;
}

void qdp_do_tick(QDPHandle *handle) {
    // Process incoming messages
    {
        QDPMessage msg = qdp_message_from_buffer(handle->rx_buffer);
        handle->recv.fn(&msg, handle->recv.ctx);

        if (qdp_verify_crc32(&msg)) {
            switch (msg.header->payload_type) {
                case PAYLOAD_TYPE_TELEMETRY_REQUEST:
                    for (int i = 0; i < handle->total_devices; i++) {
                        QDPDevice* device = &handle->devices[i];
                        if (device->device_id == msg.header->to) {
                            if (device->data_change_callback.fn != NULL) {
                                device->data_change_callback.fn(&msg, device->data_change_callback.ctx);
                                qdp_swap_from_to(&msg);
                                msg.header->payload_type = PAYLOAD_TYPE_TELEMETRY_RESPONSE;
                                qdp_update_crc32(&msg);
                                handle->send.fn(&msg, handle->send.ctx);
                            }
                            break;
                        }
                    }
                    break;
                case PAYLOAD_TYPE_DEVICE_ID_REQUEST:
                    qdp_swap_from_to(&msg);
                    qdp_payload_clear_array(msg.payload);
                    msg.header->payload_type = PAYLOAD_TYPE_DEVICE_ID_RESPONSE;

                    for (int i = 0; i < handle->total_devices; i++) {
                        QDPDevice* device = &handle->devices[i];
                        qdp_payload_append_uint(msg.payload, handle->devices[i].device_id);
                    }

                    qdp_update_crc32(&msg);
                    handle->send.fn(&msg, handle->send.ctx);
                    break;
                default:
                    if (handle->on_event_or_response.fn != NULL) {
                        handle->on_event_or_response.fn(&msg, handle->on_event_or_response.ctx);
                    }
                    break;
            }
        }
    }

    // Process outgoing messages
    {
        QDPMessage msg = qdp_message_from_buffer(handle->tx_buffer);

        // iterate through each device and call the data change callback
        for (int i = 0; i < handle->total_devices; i++) {
            QDPDevice* device = &handle->devices[i];

            if (device->data_change_callback.fn == NULL) {
                continue;
            }

            // if the data changes, send the message
            if (device->data_change_callback.fn(&msg, device->data_change_callback.ctx) ) {
                msg.header->from = device->device_id;
                msg.header->to = 0; // broadcast
                qdp_update_crc32(&msg);
                handle->send.fn(&msg, handle->send.ctx);
            }
        }
    }
}

QDPHandle qdp_init() {
    qdp_generate_crc32_table();

    QDPHandle handle;
    handle.send.fn = NULL;
    handle.recv.fn = NULL;
    handle.on_command_request.fn = NULL;
    handle.on_event_or_response.fn = NULL;
    handle.total_devices = 0;

    return handle;
}
