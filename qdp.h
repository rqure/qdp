#pragma once

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdio.h>

#define QDP_MAX_BUFFER_SIZE 1024 * 10
#define QDP_MAX_DEVICES 64

#ifndef QDP_MALLOC
#define QDP_MALLOC(size) malloc(size)
#endif

#ifndef QDP_FREE
#define QDP_FREE(ptr) free(ptr)
#endif

// Enum for payload types
typedef enum
{
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
typedef enum
{
    DATA_TYPE_NULL,   // No data
    DATA_TYPE_INT,    // 32-bit signed integer
    DATA_TYPE_UINT,   // 32-bit unsigned integer
    DATA_TYPE_FLOAT,  // 32-bit floating-point
    DATA_TYPE_STRING, // Variable-length string
    DATA_TYPE_ARRAY   // Array of data
} QDPDataType;

// Enum for message validity
typedef enum
{
    QDP_MESSAGE_INCOMPLETE,
    QDP_MESSAGE_INVALID,
    QDP_MESSAGE_VALID
} QDPMessageValidity;

// Struct for data contained in the payload
typedef struct
{
    uint32_t data_type; // Type of the data
    uint32_t size;      // Size of the content
    void *content;      // Pointer to the content (can be of any data_type)
} QDPPayload;

// Header struct
typedef struct
{
    uint32_t from;           // Source device ID
    uint32_t to;             // Target device ID (0 for broadcast)
    uint32_t payload_type;   // Type of payload
    uint32_t correlation_id; // Correlation ID for tracking responses
} QDPHeader;

// Message struct
typedef struct
{
    uint8_t *buffer;             // Pointer to the message buffer
    QDPHeader *header;           // Pointer to dynamically allocated header
    QDPPayload *payload;         // Pointer to dynamically allocated payload
    uint32_t *checksum;          // Checksum/CRC for data integrity
    uint32_t size;               // Size of the message
    uint32_t max_size;           // Maximum size of the message buffer
    QDPMessageValidity validity; // Validity of the message
} QDPMessage;

// Callback for sending the message
typedef struct
{
    // Function pointer to the callback function
    // Returns 0 on success, otherwise an error code indicating failure
    int (*fn)(QDPMessage *, void *);
    void *ctx;
} QDPSendCallback;

// Callback for receiving the message
typedef struct
{
    // Function pointer to the callback function
    // Returns 0 on success, otherwise an error code indicating failure
    int (*fn)(QDPMessage *, void *);
    void *ctx;
} QDPRecvCallback;

// Callback for getting the device data
typedef struct
{
    // Function pointer to the callback function
    // Updates the payload with the latest data
    void (*fn)(QDPPayload *, void *);
    void *ctx;
} QDPGetCallback;

// Callback for setting the device data and performing any additional actions
typedef struct
{
    // Function pointer to the callback function
    // Sets the device data and performs any additional actions
    void (*fn)(QDPPayload *, void *);
    void *ctx;
} QDPSetCallback;

// Callback for handling event messages designated for the device or response messages
typedef struct
{
    // Function pointer to the callback function
    // Handles event messages designated for the device or response messages
    void (*fn)(const QDPMessage *, void *);
    void *ctx;
} QDPEventCallback;

// Callback for checking if the data has changed
typedef struct
{
    // Function pointer to the callback function
    // Checks if the data has changed
    // Returns true if the data has changed, otherwise false
    bool (*fn)(void *);
    void *ctx;
} QDPChangedCallback;

// Device struct
typedef struct
{
    uint32_t id;

    // Used to update the payload with the latest data
    QDPGetCallback get;

    // Used to set the device data and perform any additional actions
    QDPSetCallback set;

    // Used to handle event messages designated for the device or response messages
    QDPEventCallback event;

    // Used to check if the data has changed
    QDPChangedCallback changed;
} QDPDevice;

// Handle struct
typedef struct
{
    QDPDevice root_device;

    QDPSendCallback send;
    QDPRecvCallback recv;

    QDPDevice devices[QDP_MAX_DEVICES];
    uint32_t total_devices;

    QDPMessage rx_msg;
    uint8_t rx_buffer[QDP_MAX_BUFFER_SIZE];

    QDPMessage tx_msg;
    uint8_t tx_buffer[QDP_MAX_BUFFER_SIZE];
} QDPHandle;

uint32_t qdp_payload_calc_length(const QDPPayload *payload) {
    return sizeof(QDPPayload) - sizeof(void *) + payload->size;
}

uint32_t qdp_message_calc_length_no_checksum(const QDPMessage *msg) {
    return sizeof(QDPHeader) + qdp_payload_calc_length(msg->payload);
}

uint32_t qdp_message_calc_length(const QDPMessage *msg) {
    return qdp_message_calc_length_no_checksum(msg) + sizeof(uint32_t);
}

void qdp_message_init_from_buffer(QDPMessage *msg, uint8_t *buffer, uint32_t buffer_size)
{
    memset(buffer, 0, buffer_size);
    msg->size = 0;
    msg->max_size = buffer_size;

    msg->buffer = buffer;
    msg->header = (QDPHeader *)buffer;

    msg->payload = (QDPPayload *)(buffer + sizeof(QDPHeader));
    msg->payload->content = (void *)(buffer + sizeof(QDPHeader) + sizeof(QDPPayload) - sizeof(void *));

    msg->checksum = (uint32_t *)(msg->payload->content + msg->payload->size);

    msg->validity = QDP_MESSAGE_INCOMPLETE;
}

void qdp_message_consume(QDPMessage *msg)
{
    if (msg->size > msg->max_size)
    {
        msg->size = 0;
        return;
    }
    
    memmove(msg->buffer, msg->buffer + msg->size, msg->max_size - msg->size);
    msg->size = 0;
}

void qdp_message_copy(QDPMessage *dest, const QDPMessage *src)
{
    uint32_t size = src->size;
    if (dest->max_size < src->size)
    {
        size = dest->max_size;
    }

    memcpy(dest->buffer, src->buffer, size);
    dest->size = src->size;
    dest->validity = src->validity;
}

int32_t qdp_payload_get_int(QDPPayload *payload)
{
    if (payload == NULL || payload->data_type != DATA_TYPE_INT)
    {
        return 0;
    }

    return *((int32_t *)payload->content);
}

uint32_t qdp_payload_get_uint(QDPPayload *payload)
{
    if (payload == NULL || payload->data_type != DATA_TYPE_UINT)
    {
        return 0;
    }

    return *((uint32_t *)payload->content);
}

float qdp_payload_get_float(QDPPayload *payload)
{
    if (payload == NULL || payload->data_type != DATA_TYPE_FLOAT)
    {
        return 0.0;
    }

    return *((float *)payload->content);
}

char *qdp_payload_get_string(QDPPayload *payload)
{
    if (payload == NULL || payload->data_type != DATA_TYPE_STRING)
    {
        return NULL;
    }

    return (char *)payload->content;
}

QDPPayload *qdp_payload_get_array_index(QDPPayload *payload, uint32_t index)
{
    if (payload->data_type != DATA_TYPE_ARRAY)
    {
        return NULL;
    }

    uint32_t offset = 0;
    for (uint32_t i = 0; i < index; i++)
    {
        if (offset >= payload->size)
        {
            return NULL;
        }

        QDPPayload *payload_i = (QDPPayload *)(payload->content + offset);
        offset += sizeof(QDPPayload) - sizeof(void *) + payload_i->size;
    }

    return (QDPPayload *)(payload->content + offset);
}

void qdp_payload_set_null(QDPPayload *payload)
{
    payload->data_type = DATA_TYPE_NULL;
    payload->size = 0;
}

void qdp_payload_reset(QDPPayload *payload)
{
    qdp_payload_set_null(payload);
}

void qdp_payload_clear_array(QDPPayload *payload)
{
    payload->data_type = DATA_TYPE_ARRAY;
    payload->size = 0;
}

void qdp_payload_set_int(QDPPayload *payload, int32_t value)
{
    payload->data_type = DATA_TYPE_INT;
    payload->size = sizeof(int32_t);
    memcpy(payload->content, &value, payload->size);
}

void qdp_payload_set_uint(QDPPayload *payload, uint32_t value)
{
    payload->data_type = DATA_TYPE_UINT;
    payload->size = sizeof(uint32_t);
    memcpy(payload->content, &value, payload->size);
}

void qdp_payload_set_float(QDPPayload *payload, float value)
{
    payload->data_type = DATA_TYPE_FLOAT;
    payload->size = sizeof(float);
    memcpy(payload->content, &value, payload->size);
}

void qdp_payload_set_string(QDPPayload *payload, char *value)
{
    payload->data_type = DATA_TYPE_STRING;
    payload->size = strlen(value) + 1;
    memcpy(payload->content, value, payload->size);
}

void qdp_payload_set_array(QDPPayload *payload, QDPPayload *array, uint32_t count)
{
    payload->data_type = DATA_TYPE_ARRAY;
    payload->size = 0;
    for (uint32_t i = 0; i < count; i++)
    {
        QDPPayload *payload_i = (QDPPayload *)(payload->content + payload->size);
        payload_i->data_type = array[i].data_type;
        payload_i->size = array[i].size;
        memcpy(payload_i->content, array[i].content, array[i].size);
        payload->size += sizeof(QDPPayload) - sizeof(void *) + array[i].size;
    }
}

void qdp_payload_append_int(QDPPayload *payload, int32_t value)
{
    QDPPayload *new_payload = (QDPPayload *)(payload->content + payload->size);
    qdp_payload_set_int(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}

void qdp_payload_append_uint(QDPPayload *payload, uint32_t value)
{
    QDPPayload *new_payload = (QDPPayload *)(payload->content + payload->size);
    qdp_payload_set_uint(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}

void qdp_payload_append_float(QDPPayload *payload, float value)
{
    QDPPayload *new_payload = (QDPPayload *)(payload->content + payload->size);
    qdp_payload_set_float(new_payload, value);
    payload->size += sizeof(QDPPayload) - sizeof(void *) + new_payload->size;
}

void qdp_payload_append_string(QDPPayload *payload, char *value)
{
    QDPPayload *new_payload = (QDPPayload *)(payload->content + payload->size);
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

void qdp_crc32_generate_table()
{
    for (int i = 0; i < 256; i++)
    {
        uint32_t crc = i;
        for (int j = 0; j < 8; j++)
        {
            crc = (crc & 1) ? (crc >> 1) ^ QDP_POLYNOMIAL : crc >> 1;
        }
        qdp_crc32_table[i] = crc;
    }
}

uint32_t qdp_crc32_calculate(const QDPMessage *msg)
{
    uint8_t *data = msg->buffer;
    uint32_t length = qdp_message_calc_length_no_checksum(msg);

    uint32_t crc = 0xFFFFFFFF;
    for (size_t i = 0; i < length; i++)
    {
        crc = (crc >> 8) ^ qdp_crc32_table[(crc ^ data[i]) & 0xFF];
    }

    return crc ^ 0xFFFFFFFF;
}

void qdp_crc32_update(const QDPMessage *msg)
{
    *msg->checksum = qdp_crc32_calculate(msg);
}

bool qdp_crc32_verify(const QDPMessage *msg)
{
    return *msg->checksum == qdp_crc32_calculate(msg);
}

void qdp_message_update_validity(QDPMessage *msg)
{
    msg->validity = QDP_MESSAGE_INCOMPLETE;

    if (msg->size >= qdp_message_calc_length(msg))
    {
        msg->validity = QDP_MESSAGE_VALID;

        if (msg->size > msg->max_size) {
            msg->validity = QDP_MESSAGE_INVALID;
        }

        if (!qdp_crc32_verify(msg))
        {
            msg->validity = QDP_MESSAGE_INVALID;
        }
    }
}

void qdp_header_swap_from_to(QDPHeader *header)
{
    uint32_t temp = header->from;
    header->from = header->to;
    header->to = temp;
}

void qdp_message_complete(QDPMessage *msg)
{
    qdp_crc32_update(msg);
    msg->size = qdp_message_calc_length(msg);
    msg->validity = QDP_MESSAGE_VALID;
}

QDPDevice *qdp_device_register(QDPHandle *handle, uint32_t id)
{
    if (handle->total_devices >= QDP_MAX_DEVICES)
    {
        return NULL;
    }

    QDPDevice *device = &handle->devices[handle->total_devices];

    device->id = id;
    device->get.fn = NULL;
    device->set.fn = NULL;
    device->event.fn = NULL;
    device->changed.fn = NULL;

    handle->total_devices++;

    return device;
}

QDPDevice *qdp_device_get(QDPHandle *handle, uint32_t id)
{
    if (id == handle->root_device.id)
    {
        return &handle->root_device;
    }

    for (int i = 0; i < handle->total_devices; i++)
    {
        if (handle->devices[i].id == id)
        {
            return &handle->devices[i];
        }
    }

    return NULL;
}

int qdp_do_tick(QDPHandle *handle)
{
    // Process incoming messages
    while (true)
    {
        QDPMessage *msg = &handle->rx_msg;

        if (handle->recv.fn != NULL)
        {
            int err = handle->recv.fn(msg, handle->recv.ctx);
            if (err != 0)
            {
                return err;
            }
        }
        else
        {
            break;
        }

        qdp_message_update_validity(msg);
        if (msg->validity == QDP_MESSAGE_INVALID)
        {
            qdp_message_consume(msg);
        }

        if (msg->validity != QDP_MESSAGE_VALID)
        {
            break;
        }

        // msg is valid
        switch (msg->header->payload_type)
        {
        case PAYLOAD_TYPE_TELEMETRY_REQUEST:
            for (int i = 0; i < handle->total_devices; i++)
            {
                QDPDevice *device = &handle->devices[i];
                if (device->id == msg->header->to || msg->header->to == 0)
                {
                    QDPMessage *rsp = &handle->tx_msg;
                    qdp_message_copy(rsp, msg);

                    if (device->get.fn != NULL)
                    {
                        device->get.fn(rsp->payload, device->get.ctx);

                        rsp->header->payload_type = PAYLOAD_TYPE_TELEMETRY_RESPONSE;
                        qdp_header_swap_from_to(rsp->header);
                        if (rsp->header->from == 0)
                        {
                            rsp->header->from = device->id;
                        }
                        qdp_message_complete(rsp);
                        if (handle->send.fn != NULL)
                        {
                            int err = handle->send.fn(rsp, handle->send.ctx);
                            if (err != 0)
                            {
                                return err;
                            }
                        }
                    }

                    if (rsp->header->to != 0)
                    {
                        break;
                    }
                }
            }
            break;
        case PAYLOAD_TYPE_DEVICE_ID_REQUEST:
            if (msg->header->to != handle->root_device.id)
            {
                break;
            }

            QDPMessage *rsp = &handle->tx_msg;
            qdp_message_copy(rsp, msg);

            qdp_header_swap_from_to(rsp->header);
            qdp_payload_clear_array(rsp->payload);
            rsp->header->payload_type = PAYLOAD_TYPE_DEVICE_ID_RESPONSE;

            qdp_payload_append_uint(rsp->payload, handle->root_device.id);
            for (int i = 0; i < handle->total_devices; i++)
            {
                QDPDevice *device = &handle->devices[i];
                qdp_payload_append_uint(rsp->payload, device->id);
            }

            qdp_message_complete(rsp);
            if (handle->send.fn != NULL)
            {
                int err = handle->send.fn(rsp, handle->send.ctx);
                if (err != 0)
                {
                    return err;
                }
            }
            break;
        case PAYLOAD_TYPE_COMMAND_REQUEST:
            if (msg->header->to == handle->root_device.id || msg->header->to == 0)
            {
                QDPMessage *rsp = &handle->tx_msg;
                qdp_message_copy(rsp, msg);

                if (handle->root_device.set.fn != NULL)
                {
                    handle->root_device.set.fn(rsp->payload, handle->root_device.set.ctx);

                    rsp->header->payload_type = PAYLOAD_TYPE_COMMAND_RESPONSE;
                    qdp_header_swap_from_to(rsp->header);
                    if (rsp->header->from == 0)
                    {
                        rsp->header->from = handle->root_device.id;
                    }
                    qdp_message_complete(rsp);
                    if (handle->send.fn != NULL)
                    {
                        int err = handle->send.fn(rsp, handle->send.ctx);
                        if (err != 0)
                        {
                            return err;
                        }
                    }
                }
            }

            for (int i = 0; i < handle->total_devices; i++)
            {
                QDPDevice *device = &handle->devices[i];
                if (device->id == msg->header->to || msg->header->to == 0)
                {
                    QDPMessage *rsp = &handle->tx_msg;
                    qdp_message_copy(rsp, msg);

                    if (device->set.fn != NULL)
                    {
                        device->set.fn(rsp->payload, device->set.ctx);

                        rsp->header->payload_type = PAYLOAD_TYPE_COMMAND_RESPONSE;
                        qdp_header_swap_from_to(rsp->header);
                        if (rsp->header->from == 0)
                        {
                            rsp->header->from = device->id;
                        }
                        qdp_message_complete(rsp);
                        if (handle->send.fn != NULL)
                        {
                            int err = handle->send.fn(rsp, handle->send.ctx);
                            if (err != 0)
                            {
                                return err;
                            }
                        }
                    }

                    if (rsp->header->to != 0)
                    {
                        break;
                    }
                }
            }

            break;
        default:
            if (msg->header->to == handle->root_device.id || msg->header->to == 0)
            {
                if (handle->root_device.event.fn != NULL)
                {
                    handle->root_device.event.fn(msg, handle->root_device.event.ctx);
                }
            }

            for (int i = 0; i < handle->total_devices; i++)
            {
                QDPDevice *device = &handle->devices[i];
                if (device->id == msg->header->to || msg->header->to == 0)
                {
                    if (device->event.fn != NULL)
                    {
                        device->event.fn(msg, device->event.ctx);
                    }

                    if (msg->header->to != 0)
                    {
                        break;
                    }
                }
            }
            break;
        }

        // Clear the message buffer
        msg->size = 0;
    }

    // Process outgoing messages
    {
        QDPMessage *msg = &handle->tx_msg;

        // iterate through each device and call the data change callback
        for (int i = 0; i < handle->total_devices; i++)
        {
            QDPDevice *device = &handle->devices[i];

            if (device->changed.fn == NULL || device->get.fn == NULL)
            {
                continue;
            }

            // if the data changes, send the message
            if (device->changed.fn(device->changed.ctx))
            {
                msg->header->from = device->id;
                msg->header->to = 0; // broadcast
                msg->header->payload_type = PAYLOAD_TYPE_TELEMETRY_EVENT;
                msg->header->correlation_id = 0;
                device->get.fn(msg->payload, device->get.ctx);
                qdp_message_complete(msg);

                if (handle->send.fn != NULL)
                {
                    int err = handle->send.fn(msg, handle->send.ctx);
                    if (err != 0)
                    {
                        return err;
                    }
                }
            }
        }
    }

    return 0;
}

void qdp_init(QDPHandle *handle, uint32_t root_device_id) {
    qdp_crc32_generate_table();

    handle->root_device.id = root_device_id;
    handle->root_device.get.fn = NULL;
    handle->root_device.set.fn = NULL;
    handle->root_device.event.fn = NULL;
    handle->root_device.changed.fn = NULL;

    handle->send.fn = NULL;
    handle->recv.fn = NULL;
    handle->total_devices = 0;

    qdp_message_init_from_buffer(&handle->rx_msg, handle->rx_buffer, sizeof(handle->rx_buffer));
    qdp_message_init_from_buffer(&handle->tx_msg, handle->tx_buffer, sizeof(handle->rx_buffer));
}

void qdp_malloc_init(QDPHandle **handle_ptr, uint32_t root_device_id)
{
    QDPHandle *handle = (QDPHandle *)QDP_MALLOC(sizeof(QDPHandle));
    *handle_ptr = handle;
    if (handle == NULL)
    {
        return;
    }

    qdp_init(handle, root_device_id);
}
