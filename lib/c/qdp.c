#include "qdp.h"

#ifdef QDP_DEBUG
#define QDP_DEBUG_PRINT(...) fprintf(stderr, __VA_ARGS__)
#else
#define QDP_DEBUG_PRINT(...) ((void)0)
#endif

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

// Add these helper functions for endian-safe operations
static void write_uint32_le(uint8_t* buf, uint32_t value) {
    buf[0] = (uint8_t)(value & 0xFF);
    buf[1] = (uint8_t)((value >> 8) & 0xFF);
    buf[2] = (uint8_t)((value >> 16) & 0xFF);
    buf[3] = (uint8_t)((value >> 24) & 0xFF);
}

static uint32_t read_uint32_le(const uint8_t* buf) {
    return ((uint32_t)buf[0]) |
           ((uint32_t)buf[1] << 8) |
           ((uint32_t)buf[2] << 16) |
           ((uint32_t)buf[3] << 24);
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
    // Calculate required size
    size_t header_size = 8;  // Just the lengths (4 + 4)
    size_t total_size = header_size + msg->header.topic_len + msg->payload.size + sizeof(uint32_t);
    
    QDP_DEBUG_PRINT("[QDP-WRITE] Writing message:\n");
    QDP_DEBUG_PRINT("  Topic: %s (len=%u)\n", msg->header.topic, msg->header.topic_len);
    QDP_DEBUG_PRINT("  Payload size: %u\n", msg->payload.size);
    QDP_DEBUG_PRINT("  Total size: %zu\n", total_size);
    
    if (!qdp_buffer_can_write(buf, total_size)) {
        QDP_DEBUG_PRINT("[QDP-WRITE] Buffer too small (capacity=%zu)\n", buf->capacity);
        return false;
    }

    size_t start_pos = buf->size;

    // Write lengths in little-endian
    write_uint32_le(buf->data + buf->size, msg->header.topic_len);
    buf->size += 4;
    write_uint32_le(buf->data + buf->size, msg->payload.size);
    buf->size += 4;

    // Write topic string
    memcpy(buf->data + buf->size, msg->header.topic, msg->header.topic_len);
    buf->size += msg->header.topic_len;

    // Write payload
    memcpy(buf->data + buf->size, msg->payload.data, msg->payload.size);
    buf->size += msg->payload.size;

    // Calculate CRC on everything before CRC position
    uint32_t crc = qdp_calc_crc32(buf->data + start_pos, buf->size - start_pos);
    write_uint32_le(buf->data + buf->size, crc);
    buf->size += sizeof(uint32_t);

    QDP_DEBUG_PRINT("[QDP-WRITE] Message written successfully (%zu bytes)\n", buf->size - start_pos);
    QDP_DEBUG_PRINT("[QDP-WRITE] CRC: %u\n", crc);
    
    return true;
}

bool qdp_message_read(qdp_buffer_t *buf, qdp_message_t *msg) {
    size_t start_pos = buf->position;
    
    QDP_DEBUG_PRINT("[QDP-READ] Attempting to read message at position %zu of %zu bytes\n", 
            buf->position, buf->size);

    // Dump first 16 bytes of buffer for debugging
    QDP_DEBUG_PRINT("[QDP-READ] Buffer head: ");
    for(size_t i = 0; i < 16 && (buf->position + i) < buf->size; i++) {
        QDP_DEBUG_PRINT("%02X ", buf->data[buf->position + i]);
    }
    QDP_DEBUG_PRINT("\n");

    // Read lengths (8 bytes total)
    if (!qdp_buffer_can_read(buf, 8)) {
        QDP_DEBUG_PRINT("[QDP-READ] Buffer too small for message header\n");
        return false;
    }

    // Read lengths in little-endian
    msg->header.topic_len = read_uint32_le(buf->data + buf->position);
    buf->position += 4;
    msg->header.payload_len = read_uint32_le(buf->data + buf->position);
    buf->position += 4;

    QDP_DEBUG_PRINT("[QDP-READ] Header read:\n");
    QDP_DEBUG_PRINT("  Topic length: %u\n", msg->header.topic_len);
    QDP_DEBUG_PRINT("  Payload length: %u\n", msg->header.payload_len);

    // Validate lengths
    if (msg->header.topic_len > QDP_MAX_TOPIC_LENGTH ||
        msg->header.payload_len > QDP_MAX_PAYLOAD_SIZE) {
        QDP_DEBUG_PRINT("[QDP-READ] Message exceeds max sizes (topic: %u/%u, payload: %u/%u)\n",
                msg->header.topic_len, QDP_MAX_TOPIC_LENGTH,
                msg->header.payload_len, QDP_MAX_PAYLOAD_SIZE);
        return false;
    }

    // Calculate remaining bytes from the original buffer size, not current position
    size_t total_message_size = 8 + msg->header.topic_len + msg->header.payload_len + sizeof(uint32_t);
    if (buf->size < total_message_size) {
        QDP_DEBUG_PRINT("[QDP-READ] Incomplete message (have %zu bytes, need %zu)\n",
                buf->size, total_message_size);
        return false;
    }

    // Read topic
    memcpy(msg->header.topic, buf->data + buf->position, msg->header.topic_len);
    msg->header.topic[msg->header.topic_len] = '\0';  // Null terminate
    buf->position += msg->header.topic_len;

    // Read payload
    memcpy(msg->payload.data, buf->data + buf->position, msg->header.payload_len);
    msg->payload.size = msg->header.payload_len;
    buf->position += msg->header.payload_len;

    // Read CRC in little-endian
    msg->checksum = read_uint32_le(buf->data + buf->position);
    buf->position += sizeof(uint32_t);

    // Verify CRC
    uint32_t calc_crc = qdp_calc_crc32(buf->data + start_pos, 
                                      buf->position - start_pos - sizeof(uint32_t));
    
    if (msg->checksum != calc_crc) {
        QDP_DEBUG_PRINT("[QDP] CRC mismatch - expected: %u, got: %u\n", msg->checksum, calc_crc);
        return false;
    }

    return true;
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
    if (!qdp->transport.recv) {
        QDP_DEBUG_PRINT("[QDP] No receive handler registered\n");
        return 0;
    }

    // Reset stream for new messages
    qdp_stream_begin_message(&qdp->rx);

    // Get new messages from transport
    int err = qdp->transport.recv(&qdp->rx.buffer, qdp->transport.ctx);
    if (err != 0) {
        QDP_DEBUG_PRINT("[QDP] Transport receive error: %d\n", err);
        return err;
    }

    if (qdp->rx.buffer.size == 0) {
        return 0;  // No data received
    }
    
    QDP_DEBUG_PRINT("[QDP] Received %zu bytes\n", qdp->rx.buffer.size);

    // Process messages using stream API
    qdp_message_t msg = {0};
    while (qdp_stream_next_message(&qdp->rx, &msg)) {
        QDP_DEBUG_PRINT("[QDP] Processing message: topic='%s' payload_len=%u\n", 
                msg.header.topic, msg.header.payload_len);
                
        // Match against subscriptions
        bool matched = false;
        for (uint32_t i = 0; i < qdp->sub_count; i++) {
            if (qdp_topic_matches(qdp->subs[i].topic, msg.header.topic)) {
                QDP_DEBUG_PRINT("[QDP] Message matched subscription '%s'\n", qdp->subs[i].topic);
                qdp->subs[i].callback.fn(&msg, qdp->subs[i].callback.ctx);
                matched = true;
            }
        }
        
        if (!matched) {
            QDP_DEBUG_PRINT("[QDP] No matching subscriptions for topic '%s'\n", msg.header.topic);
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