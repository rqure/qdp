#include "qdp.h"

// CRC table and initialization
static uint32_t qdp_crc32_table[256];

static void qdp_crc32_init(void) {
    for (int i = 0; i < 256; i++) {
        uint32_t crc = i;
        for (int j = 0; j < 8; j++) {
            crc = (crc & 1) ? (crc >> 1) ^ 0xEDB88320 : crc >> 1;
        }
        qdp_crc32_table[i] = crc;
    }
}

static uint32_t qdp_calc_crc32(const uint8_t *data, size_t length) {
    uint32_t crc = 0xFFFFFFFF;
    for (size_t i = 0; i < length; i++) {
        crc = (crc >> 8) ^ qdp_crc32_table[(crc ^ data[i]) & 0xFF];
    }
    return crc ^ 0xFFFFFFFF;
}

// Buffer operations
qdp_buffer_t qdp_buffer_create(uint8_t *data, size_t capacity) {
    return (qdp_buffer_t){
        .data = data,
        .size = 0,
        .capacity = capacity,
        .position = 0
    };
}

void qdp_buffer_reset(qdp_buffer_t *buf) {
    buf->size = 0;
    buf->position = 0;
}

bool qdp_buffer_can_read(const qdp_buffer_t *buf, size_t bytes) {
    return (buf->position + bytes) <= buf->size;
}

bool qdp_buffer_can_write(const qdp_buffer_t *buf, size_t bytes) {
    return (buf->size + bytes) <= buf->capacity;
}

// Message operations
bool qdp_message_write(qdp_buffer_t *buf, const qdp_message_t *msg) {
    // Write header and raw payload
    size_t total_size = sizeof(qdp_header_t) + msg->payload.size;
    if (!qdp_buffer_can_write(buf, total_size + sizeof(uint32_t))) {
        return false;
    }

    memcpy(buf->data + buf->size, &msg->header, sizeof(qdp_header_t));
    buf->size += sizeof(qdp_header_t);

    memcpy(buf->data + buf->size, msg->payload.data, msg->payload.size);
    buf->size += msg->payload.size;

    uint32_t crc = qdp_calc_crc32(buf->data + buf->position, buf->size - buf->position);
    memcpy(buf->data + buf->size, &crc, sizeof(uint32_t));
    buf->size += sizeof(uint32_t);

    return true;
}

bool qdp_message_read(qdp_buffer_t *buf, qdp_message_t *msg) {
    if (!qdp_buffer_can_read(buf, sizeof(qdp_header_t))) {
        return false;
    }

    // Read header
    memcpy(&msg->header, buf->data + buf->position, sizeof(qdp_header_t));
    buf->position += sizeof(qdp_header_t);

    // Check if we can read the full message
    if (!qdp_buffer_can_read(buf, msg->header.payload_len + sizeof(uint32_t))) {
        return false;
    }

    // Read raw payload
    if (msg->header.payload_len > QDP_MAX_PAYLOAD_SIZE) {
        return false;
    }
    
    memcpy(msg->payload.data, buf->data + buf->position, msg->header.payload_len);
    msg->payload.size = msg->header.payload_len;
    buf->position += msg->header.payload_len;

    // Read and verify checksum
    if (!qdp_buffer_can_read(buf, sizeof(uint32_t))) {
        return false;
    }

    msg->checksum = *(uint32_t*)(buf->data + buf->position);
    buf->position += sizeof(uint32_t);

    uint32_t calc_crc = qdp_calc_crc32(buf->data + buf->position - 
                                      msg->header.payload_len - 
                                      sizeof(qdp_header_t) - 
                                      sizeof(uint32_t),
                                      msg->header.payload_len + sizeof(qdp_header_t));
    
    return msg->checksum == calc_crc;
}

// Modified publish operations
int qdp_publish_string(qdp_t* qdp, const char* topic, const char* str) {
    qdp_stream_begin_message(&qdp->tx);
    
    qdp_message_t msg = {0};
    msg.header.topic_len = strlen(topic);
    msg.header.payload_len = strlen(str);
    strncpy(msg.header.topic, topic, QDP_MAX_TOPIC_LENGTH-1);
    
    size_t len = strlen(str);
    if (len > QDP_MAX_PAYLOAD_SIZE) return -1;
    memcpy(msg.payload.data, str, len);
    msg.payload.size = len;
    
    if (!qdp_message_write(&qdp->tx.buffer, &msg)) {
        return -1;
    }
    
    return qdp->transport.send ? qdp->transport.send(&qdp->tx.buffer, qdp->transport.ctx) : 0;
}

void qdp_message_clear(qdp_message_t *msg) {
    memset(&msg->header, 0, sizeof(qdp_header_t));
    memset(&msg->payload, 0, sizeof(qdp_payload_t));
    msg->checksum = 0;
}

uint32_t qdp_message_calc_size(const qdp_message_t *msg) {
    return sizeof(qdp_header_t) + msg->header.payload_len + sizeof(uint32_t);
}

// Stream operations
bool qdp_stream_begin_message(qdp_stream_t *stream) {
    qdp_buffer_reset(&stream->buffer);
    return true;
}

bool qdp_stream_end_message(qdp_stream_t *stream) {
    return stream->buffer.size > 0;
}

bool qdp_stream_next_message(qdp_stream_t *stream, qdp_message_t *msg) {
    // Don't try to read if we've reached the end
    if (stream->buffer.position >= stream->buffer.size) {
        return false;
    }
    return qdp_message_read(&stream->buffer, msg);
}

// Public API implementation
qdp_t* qdp_new(void) {
    qdp_t* qdp = QDP_MALLOC(sizeof(qdp_t));
    if (!qdp) return NULL;

    memset(qdp, 0, sizeof(qdp_t));
    qdp->rx.buffer = qdp_buffer_create(qdp->rx_data, QDP_MAX_BUFFER_CAPACITY);
    qdp->tx.buffer = qdp_buffer_create(qdp->tx_data, QDP_MAX_BUFFER_CAPACITY);
    
    qdp_crc32_init();
    return qdp;
}

void qdp_free(qdp_t* qdp) {
    if (qdp) QDP_FREE(qdp);
}

void qdp_set_transport(qdp_t* qdp, int (*send)(qdp_buffer_t*, void*),
                      int (*recv)(qdp_buffer_t*, void*), void* ctx) {
    qdp->transport.send = send;
    qdp->transport.recv = recv;
    qdp->transport.ctx = ctx;
}

bool qdp_subscribe(qdp_t* qdp, const char* pattern,
                  void (*callback)(const qdp_message_t*, void*), void* ctx) {
    if (qdp->sub_count >= QDP_MAX_SUBSCRIPTIONS) return false;
    
    qdp_subscription_t* sub = &qdp->subs[qdp->sub_count++];
    strncpy(sub->topic, pattern, QDP_MAX_TOPIC_LENGTH-1);
    sub->callback.fn = callback;
    sub->callback.ctx = ctx;
    return true;
}

void qdp_unsubscribe(qdp_t* qdp, const char* pattern) {
    for (uint32_t i = 0; i < qdp->sub_count; i++) {
        if (strcmp(qdp->subs[i].topic, pattern) == 0) {
            if (i < qdp->sub_count - 1) {
                memmove(&qdp->subs[i], &qdp->subs[i+1], 
                       sizeof(qdp_subscription_t) * (qdp->sub_count - i - 1));
            }
            qdp->sub_count--;
            i--; // Check same index again after shift
        }
    }
}

int qdp_publish_bytes(qdp_t* qdp, const char* topic, const void* data, size_t len) {
    if (len > QDP_MAX_PAYLOAD_SIZE) return -1;
    
    qdp_stream_begin_message(&qdp->tx);
    
    qdp_message_t msg = {0};
    msg.header.topic_len = strlen(topic);
    msg.header.payload_len = len;
    strncpy(msg.header.topic, topic, QDP_MAX_TOPIC_LENGTH-1);
    
    memcpy(msg.payload.data, data, len);
    msg.payload.size = len;
    
    if (!qdp_message_write(&qdp->tx.buffer, &msg)) {
        return -1;
    }
    
    return qdp->transport.send ? qdp->transport.send(&qdp->tx.buffer, qdp->transport.ctx) : 0;
}

int qdp_process(qdp_t* qdp) {
    if (!qdp->transport.recv) return 0;

    // Reset stream for new messages
    qdp_stream_begin_message(&qdp->rx);

    // Get new messages from transport
    int err = qdp->transport.recv(&qdp->rx.buffer, qdp->transport.ctx);
    if (err != 0) return err;

    // Process messages using stream API
    qdp_message_t msg = {0};
    while (qdp_stream_next_message(&qdp->rx, &msg)) {
        // Match against subscriptions
        for (uint32_t i = 0; i < qdp->sub_count; i++) {
            if (qdp_topic_matches(qdp->subs[i].topic, msg.header.topic)) {
                qdp->subs[i].callback.fn(&msg, qdp->subs[i].callback.ctx);
            }
        }
        qdp_message_clear(&msg);
    }

    return 0;
}

// Helper functions
const char* qdp_get_topic(const qdp_message_t* msg) {
    return msg->header.topic;
}

const char* qdp_get_string(const qdp_message_t* msg) {
    if (msg->payload.size == 0) return NULL;
    return (const char*)msg->payload.data;
}

const void* qdp_get_bytes(const qdp_message_t* msg, size_t* len) {
    if (len) *len = msg->payload.size;
    return msg->payload.data;
}

bool qdp_topic_matches(const char* pattern, const char* topic) {
    const char *p = pattern;
    const char *t = topic;
    
    while (*p && *t) {
        // Handle multi-level wildcard
        if (*p == QDP_MULTI_LEVEL_WILDCARD) {
            return *(p + 1) == '\0';  // '#' must be last character
        }
        
        // Handle single-level wildcard
        if (*p == QDP_SINGLE_LEVEL_WILDCARD) {
            // Find next separator in topic
            const char* next_sep = strchr(t, QDP_TOPIC_LEVEL_SEPARATOR);
            if (!next_sep) {
                // No more separators, must match until end
                return *(p + 1) == '\0';
            }
            // Skip this level
            t = next_sep + 1;
            p++;
            if (*p == QDP_TOPIC_LEVEL_SEPARATOR) p++;
            continue;
        }
        
        // Handle separators
        if (*p == QDP_TOPIC_LEVEL_SEPARATOR) {
            if (*t != QDP_TOPIC_LEVEL_SEPARATOR) return false;
            p++;
            t++;
            continue;
        }
        
        // Match exact characters
        if (*p != *t) return false;
        p++;
        t++;
    }
    
    // Both strings must be at the end, or pattern ends with #
    return (*p == '\0' && *t == '\0') || 
           (*p == QDP_MULTI_LEVEL_WILDCARD && *(p + 1) == '\0');
}

#if defined(__linux__) && defined(__ELF__)
__asm__(".section .note.GNU-stack,\"\",@progbits");
#endif