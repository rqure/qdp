#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdio.h>

// Enum for payload types
typedef enum {
    PAYLOAD_TYPE_TELEMETRY_EVENT = 0x01,
    PAYLOAD_TYPE_TELEMETRY_REQUEST,
    PAYLOAD_TYPE_TELEMETRY_RESPONSE,
    PAYLOAD_TYPE_COMMAND_REQUEST,
    PAYLOAD_TYPE_COMMAND_RESPONSE,
    PAYLOAD_TYPE_DEVICE_ID_REQUEST,
    PAYLOAD_TYPE_DEVICE_ID_RESPONSE,
    PAYLOAD_TYPE_ERROR_RESPONSE
} QDPPayloadType;

// Enum for data types in the payload
typedef enum {
    DATA_TYPE_INT,        // 32-bit signed integer
    DATA_TYPE_UINT,       // 32-bit unsigned integer
    DATA_TYPE_FLOAT,      // 32-bit floating-point
    DATA_TYPE_STRING,     // Variable-length string
    DATA_TYPE_ARRAY       // Array of data
} QDPDataType;

// Struct for data contained in the payload
typedef struct {
    QDPDataType type;       // Type of the data
    uint32_t size;          // Size of the content
    void *content;          // Pointer to the content (can be of any type)
} QDPPayloadData;

// Header struct
typedef struct {
    uint32_t from;             // Source device ID
    uint32_t to;               // Target device ID (0 for broadcast)
    QDPPayloadType payload_type;  // Type of payload
    uint32_t correlation_id;   // Correlation ID for tracking responses
} QDPHeader;

// Payload struct
typedef struct {
    QDPPayloadData *data;      // Pointer to the payload data
} QDPPayload;

// Message struct
typedef struct {
    QDPHeader *header;        // Pointer to dynamically allocated header
    QDPPayload *payload;      // Pointer to dynamically allocated payload
    uint32_t checksum;        // Checksum/CRC for data integrity
} QDPMessage;

// Initializes QDPHeader with default values (e.g., zeroed fields)
QDPHeader *qdp_create_header() {
    QDPHeader *header = (QDPHeader *)malloc(sizeof(QDPHeader));
    if (header) {
        header->from = 0;
        header->to = 0;
        header->payload_type = PAYLOAD_TYPE_TELEMETRY_REQUEST;  // Default type
        header->correlation_id = 0;
    }
    return header;
}

// Initializes QDPPayload with default values (no data)
QDPPayload *qdp_create_payload() {
    QDPPayload *payload = (QDPPayload *)malloc(sizeof(QDPPayload));
    if (payload) {
        payload->data = NULL;  // Initially set to NULL (no data)
    }
    return payload;
}

// Initializes QDPMessage with dynamically allocated header and payload
QDPMessage *qdp_create_message() {
    QDPMessage *message = (QDPMessage *)malloc(sizeof(QDPMessage));
    if (message) {
        message->header = qdp_create_header();
        message->payload = qdp_create_payload();
        message->checksum = 0;
    }
    return message;
}

// Freeing functions to clean up allocated memory
void qdp_free_header(QDPHeader *header) {
    if (header) {
        free(header);
    }
}

void qdp_free_payload(QDPPayload *payload) {
    if (payload) {
        if (payload->data) {
            // Free the content in the payload based on its type
            if (payload->data->content) {
                free(payload->data->content); // Free the content itself
            }
            free(payload->data); // Free the payload data itself
        }
        free(payload);
    }
}

void qdp_free_message(QDPMessage *message) {
    if (message) {
        qdp_free_header(message->header); // Free the header
        qdp_free_payload(message->payload); // Free the payload
        free(message); // Free the message itself
    }
}

// Example to allocate and set data for payload
void qdp_set_payload_data(QDPPayload *payload, QDPDataType type, void *content, uint32_t size) {
    if (payload) {
        // Free existing data if it's already allocated
        if (payload->data) {
            free(payload->data->content); // Free existing content
            free(payload->data); // Free the existing payload data
        }

        // Allocate new payload data
        payload->data = (QDPPayloadData *)malloc(sizeof(QDPPayloadData));
        if (payload->data) {
            payload->data->type = type; // Set the data type
            payload->data->size = size; // Set the content size

            // Allocate memory for the content
            payload->data->content = malloc(size);
            if (payload->data->content) {
                memcpy(payload->data->content, content, size); // Copy content
            }
        }
    }
}

// Function to create a telemetry event message
QDPMessage *qdp_create_telemetry_event_message(uint32_t from, uint32_t to, QDPDataType data_type, void *data, uint32_t data_size) {
    // Create a new QDP message
    QDPMessage *message = qdp_create_message();
    if (!message) {
        return NULL; // Return NULL if message creation fails
    }

    // Set the header fields
    message->header->from = from;                        // Set source device ID
    message->header->to = to;                            // Set target device ID
    message->header->payload_type = PAYLOAD_TYPE_TELEMETRY_EVENT; // Set payload type
    message->header->correlation_id = 0;                 // Set correlation ID (optional)

    // Create the payload data for the telemetry event
    // Set the payload data based on the provided data type and content
    qdp_set_payload_data(message->payload, data_type, data, data_size);

    return message; // Return the constructed telemetry event message
}
