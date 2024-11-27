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

// Base64 lookup tables
static const char base64_chars[] = 
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
static const int base64_invs[] = {
    62, -1, -1, -1, 63, 52, 53, 54, 55, 56, 57, 58,
    59, 60, 61, -1, -1, -1, -1, -1, -1, -1, 0, 1, 2, 3, 4, 5,
    6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
    21, 22, 23, 24, 25, -1, -1, -1, -1, -1, -1, 26, 27, 28,
    29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42,
    43, 44, 45, 46, 47, 48, 49, 50, 51
};

bool qdp_base64_encode(const uint8_t* input, size_t input_len, uint8_t* output, size_t* output_len) {
    size_t enc_len = 4 * ((input_len + 2) / 3);
    if (*output_len < enc_len + 1) return false;
    *output_len = enc_len;
    
    size_t i = 0, j = 0;

    while (i < input_len) {
        uint32_t octet_a = i < input_len ? input[i++] : 0;
        uint32_t octet_b = i < input_len ? input[i++] : 0;
        uint32_t octet_c = i < input_len ? input[i++] : 0;

        uint32_t triple = (octet_a << 16) + (octet_b << 8) + octet_c;

        output[j++] = base64_chars[(triple >> 18) & 0x3F];
        output[j++] = base64_chars[(triple >> 12) & 0x3F];
        output[j++] = base64_chars[(triple >> 6) & 0x3F];
        output[j++] = base64_chars[triple & 0x3F];
    }

    if (input_len % 3 >= 1) output[enc_len - 1] = '=';
    if (input_len % 3 == 1) output[enc_len - 2] = '=';
    output[enc_len] = 0;
    
    return true;
}

bool qdp_base64_decode(const uint8_t* input, size_t input_len, uint8_t* output, size_t* output_len) {
    if (input_len % 4 != 0) return false;
    
    // Validate input characters first
    for (size_t i = 0; i < input_len; i++) {
        // Check if character is valid base64 character
        if (input[i] != '=' && 
            (input[i] < 43 || input[i] > 122 || base64_invs[input[i] - 43] == -1)) {
            return false;
        }
        
        // Padding can only appear at the end
        if (input[i] == '=' && i < input_len - 2) {
            return false;
        }
    }
    
    // Count padding characters ('=')
    size_t padding = 0;
    if (input_len > 0) {
        if (input[input_len - 1] == '=') padding++;
        if (input[input_len - 2] == '=') padding++;
    }
    
    // Calculate actual output length
    size_t dec_len = ((input_len / 4) * 3) - padding;
    if (*output_len < dec_len) return false;
    *output_len = dec_len;

    // Rest of decoding logic
    for (size_t i = 0, j = 0; i < input_len;) {
        uint32_t sextet_a = input[i] == '=' ? 0 : base64_invs[input[i] - 43]; i++;
        uint32_t sextet_b = input[i] == '=' ? 0 : base64_invs[input[i] - 43]; i++;
        uint32_t sextet_c = input[i] == '=' ? 0 : base64_invs[input[i] - 43]; i++;
        uint32_t sextet_d = input[i] == '=' ? 0 : base64_invs[input[i] - 43]; i++;

        uint32_t triple = (sextet_a << 18) + (sextet_b << 12) + 
                         (sextet_c << 6) + sextet_d;

        if (j < dec_len) output[j++] = (triple >> 16) & 0xFF;
        if (j < dec_len) output[j++] = (triple >> 8) & 0xFF;
        if (j < dec_len) output[j++] = triple & 0xFF;
    }
    
    return true;
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
    // Encode payload in stream's encode buffer
    size_t enc_len = sizeof(((qdp_stream_t*)0)->encode_buf);
    if (!qdp_base64_encode(msg->payload.data, msg->payload.size, 
                          ((qdp_stream_t*)buf)->encode_buf, &enc_len)) {
        return false;
    }

    // Update header with encoded payload length
    qdp_header_t header = msg->header;
    header.payload_len = enc_len;  // Use encoded length in header

    // Write header and encoded payload
    size_t total_size = sizeof(qdp_header_t) + enc_len;
    if (!qdp_buffer_can_write(buf, total_size + sizeof(uint32_t))) {
        return false;
    }

    memcpy(buf->data + buf->size, &header, sizeof(qdp_header_t));
    buf->size += sizeof(qdp_header_t);

    memcpy(buf->data + buf->size, ((qdp_stream_t*)buf)->encode_buf, enc_len);
    buf->size += enc_len;

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

    // Decode payload
    size_t dec_len = sizeof(msg->payload.data);
    if (!qdp_base64_decode(buf->data + buf->position, 
                          msg->header.payload_len,
                          msg->payload.data, &dec_len)) {
        return false;
    }
    
    msg->payload.size = dec_len;
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