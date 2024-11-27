#pragma once

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>

#ifndef QDP_MALLOC
#define QDP_MALLOC malloc
#endif

#ifndef QDP_FREE
#define QDP_FREE free
#endif

#ifndef QDP_MAX_BUFFER_CAPACITY
#define QDP_MAX_BUFFER_CAPACITY 1024 * 10
#endif

#ifndef QDP_MAX_SUBSCRIPTIONS
#define QDP_MAX_SUBSCRIPTIONS 64
#endif

#ifndef QDP_MAX_TOPIC_LENGTH
#define QDP_MAX_TOPIC_LENGTH 256
#endif

#ifndef QDP_MAX_PAYLOAD_SIZE
#define QDP_MAX_PAYLOAD_SIZE 512
#endif

// Topic pattern matching special characters
#define QDP_SINGLE_LEVEL_WILDCARD '+'
#define QDP_MULTI_LEVEL_WILDCARD '#'
#define QDP_TOPIC_LEVEL_SEPARATOR '/'

// Buffer management
typedef struct {
    uint8_t *data;
    size_t size;
    size_t capacity;
    size_t position;
} qdp_buffer_t;

// Core message components with fixed buffers
typedef struct {
    uint32_t topic_len;
    uint32_t payload_len;
    char topic[QDP_MAX_TOPIC_LENGTH];
} qdp_header_t;

typedef struct {
    uint32_t size;
    uint8_t data[QDP_MAX_PAYLOAD_SIZE];
} qdp_payload_t;

// Complete message structure (now owns its data)
typedef struct {
    qdp_header_t header;  // No longer a pointer
    qdp_payload_t payload; // No longer a pointer
    uint32_t checksum;
} qdp_message_t;

// Transport stream context with encoding buffer
typedef struct {
    qdp_buffer_t buffer;
    uint8_t encode_buf[QDP_MAX_PAYLOAD_SIZE * 2]; // For base64 encoding/decoding
} qdp_stream_t;

// Simplified callback system
typedef struct {
    void (*fn)(const qdp_message_t*, void*);
    void *ctx;
} qdp_callback_t;

typedef struct {
    char topic[QDP_MAX_TOPIC_LENGTH];
    qdp_callback_t callback;
} qdp_subscription_t;

// Main context with separated buffers
typedef struct {
    struct {
        int (*send)(qdp_buffer_t*, void*);
        int (*recv)(qdp_buffer_t*, void*);
        void *ctx;
    } transport;
    
    qdp_subscription_t subs[QDP_MAX_SUBSCRIPTIONS];
    uint32_t sub_count;
    
    qdp_stream_t rx;
    qdp_stream_t tx;
    uint8_t rx_data[QDP_MAX_BUFFER_CAPACITY];
    uint8_t tx_data[QDP_MAX_BUFFER_CAPACITY];
} qdp_t;

// Buffer operations
qdp_buffer_t qdp_buffer_create(uint8_t *data, size_t capacity);
void qdp_buffer_reset(qdp_buffer_t *buf);
bool qdp_buffer_can_read(const qdp_buffer_t *buf, size_t bytes);
bool qdp_buffer_can_write(const qdp_buffer_t *buf, size_t bytes);

// Base64 utilities
bool qdp_base64_encode(const uint8_t* input, size_t input_len, uint8_t* output, size_t* output_len);
bool qdp_base64_decode(const uint8_t* input, size_t input_len, uint8_t* output, size_t* output_len);

// Updated message operations
bool qdp_message_write(qdp_buffer_t *buf, const qdp_message_t *msg);
bool qdp_message_read(qdp_buffer_t *buf, qdp_message_t *msg);
void qdp_message_clear(qdp_message_t *msg);
uint32_t qdp_message_calc_size(const qdp_message_t *msg);

// Stream operations
bool qdp_stream_begin_message(qdp_stream_t *stream);
bool qdp_stream_end_message(qdp_stream_t *stream);
bool qdp_stream_next_message(qdp_stream_t *stream, qdp_message_t *msg);

// Public API (simplified)
qdp_t* qdp_new(void);
void qdp_free(qdp_t* qdp);

void qdp_set_transport(qdp_t* qdp,
                      int (*send)(qdp_buffer_t*, void*),
                      int (*recv)(qdp_buffer_t*, void*),
                      void* ctx);

// Publishing
int qdp_publish_string(qdp_t* qdp, const char* topic, const char* str);
int qdp_publish_bytes(qdp_t* qdp, const char* topic, const void* data, size_t len);

// Subscription
bool qdp_subscribe(qdp_t* qdp, 
                  const char* pattern,
                  void (*callback)(const qdp_message_t*, void*),
                  void* ctx);
void qdp_unsubscribe(qdp_t* qdp, const char* pattern);

// Processing
int qdp_process(qdp_t* qdp);

// Message access helpers
const char* qdp_get_topic(const qdp_message_t* msg);
const char* qdp_get_string(const qdp_message_t* msg);
const void* qdp_get_bytes(const qdp_message_t* msg, size_t* len);
bool qdp_topic_matches(const char* pattern, const char* topic);
