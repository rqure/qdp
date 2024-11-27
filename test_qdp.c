#include <stdio.h>
#include <assert.h>
#include <string.h>
#include "qdp.h"

#define TEST(name) static void test_##name(void)
#define RUN_TEST(name) do { \
    printf("Running %s...", #name); \
    test_##name(); \
    printf("OK\n"); \
} while(0)

// Mock transport callbacks
static int mock_send(qdp_buffer_t* buf, void* ctx) {
    qdp_buffer_t* target = (qdp_buffer_t*)ctx;
    if (target->size + buf->size > target->capacity) return -1;
    memcpy(target->data, buf->data, buf->size); // Change: copy to start of target
    target->size = buf->size;                    // Change: set exact size
    buf->size = 0;  // Reset source buffer after copying
    return 0;
}

static int mock_recv(qdp_buffer_t* buf, void* ctx) {
    qdp_buffer_t* source = (qdp_buffer_t*)ctx;
    if (source->size == 0) return -1;
    // Copy data from mock transport to rx buffer
    memcpy(buf->data, source->data, source->size);
    buf->size = source->size;
    buf->position = 0;  // Reset position
    source->size = 0;   // Clear mock transport
    return 0;
}

// Move these to file scope and make them static
static qdp_message_t received_msg;
static int callback_called = 0;

// Move callback function outside of test function
static void test_callback(const qdp_message_t* msg, void* ctx) {
    (void)ctx; // Explicitly ignore the parameter
    memcpy(&received_msg, msg, sizeof(qdp_message_t));
    callback_called = 1;
}

// Buffer Tests
TEST(buffer_operations) {
    uint8_t data[128];
    qdp_buffer_t buf = qdp_buffer_create(data, sizeof(data));
    
    assert(buf.capacity == sizeof(data));
    assert(buf.size == 0);
    assert(buf.position == 0);
    
    assert(qdp_buffer_can_write(&buf, 64));
    assert(qdp_buffer_can_write(&buf, 128));
    assert(!qdp_buffer_can_write(&buf, 129));
    
    buf.size = 100;
    assert(qdp_buffer_can_read(&buf, 50));
    assert(qdp_buffer_can_read(&buf, 100));
    assert(!qdp_buffer_can_read(&buf, 101));
    
    qdp_buffer_reset(&buf);
    assert(buf.size == 0);
    assert(buf.position == 0);
}

// Base64 Tests
TEST(base64_codec) {
    const uint8_t input[] = "Hello, World!";
    uint8_t encoded[128];
    uint8_t decoded[128];
    size_t enc_len = sizeof(encoded);
    size_t dec_len = sizeof(decoded);
    
    assert(qdp_base64_encode(input, strlen((char*)input), encoded, &enc_len));
    assert(qdp_base64_decode(encoded, enc_len, decoded, &dec_len));
    assert(dec_len == strlen((char*)input));
    assert(memcmp(input, decoded, dec_len) == 0);
}

// Message Tests
TEST(message_operations) {
    uint8_t buffer[1024];
    qdp_stream_t stream = { .buffer = {0}, .encode_buf = {0} };
    stream.buffer = qdp_buffer_create(buffer, sizeof(buffer));
    
    qdp_message_t msg = {0};
    strcpy(msg.header.topic, "test/topic");
    msg.header.topic_len = strlen(msg.header.topic);
    
    const char* payload = "test payload";
    memcpy(msg.payload.data, payload, strlen(payload));
    msg.payload.size = strlen(payload);
    msg.header.payload_len = msg.payload.size;
    
    assert(qdp_message_write(&stream.buffer, &msg));
    
    qdp_message_t read_msg = {0};
    stream.buffer.position = 0;
    assert(qdp_message_read(&stream.buffer, &read_msg));
    
    assert(strcmp(msg.header.topic, read_msg.header.topic) == 0);
    assert(msg.payload.size == read_msg.payload.size);
    assert(memcmp(msg.payload.data, read_msg.payload.data, msg.payload.size) == 0);
}

// Protocol Tests
TEST(protocol_pubsub) {
    qdp_t* qdp = qdp_new();
    uint8_t mock_buf[1024];
    qdp_buffer_t mock_transport = qdp_buffer_create(mock_buf, sizeof(mock_buf));
    
    callback_called = 0; // Reset the flag
    qdp_set_transport(qdp, mock_send, mock_recv, &mock_transport);
    
    assert(qdp_subscribe(qdp, "test/topic", test_callback, NULL));
    assert(qdp_publish_string(qdp, "test/topic", "Hello") == 0);
    
    // Simulate message processing
    mock_transport.position = 0;
    assert(qdp_process(qdp) == 0);
    assert(callback_called == 1);
    assert(strcmp(qdp_get_string(&received_msg), "Hello") == 0);
    
    qdp_free(qdp);
}

// Edge case tests
TEST(edge_cases) {
    // Test empty payload
    qdp_message_t msg = {0};
    strcpy(msg.header.topic, "test/empty");
    msg.header.topic_len = strlen(msg.header.topic);
    msg.payload.size = 0;
    
    uint8_t buffer[1024];
    qdp_stream_t stream = { .buffer = {0}, .encode_buf = {0} };
    stream.buffer = qdp_buffer_create(buffer, sizeof(buffer));
    
    assert(qdp_message_write(&stream.buffer, &msg));
    
    qdp_message_t read_msg = {0};
    stream.buffer.position = 0;
    assert(qdp_message_read(&stream.buffer, &read_msg));
    assert(read_msg.payload.size == 0);
    
    // Test maximum size payload
    msg.payload.size = QDP_MAX_PAYLOAD_SIZE;
    memset(msg.payload.data, 'X', msg.payload.size);
    
    stream.buffer.size = 0;
    stream.buffer.position = 0;
    assert(qdp_message_write(&stream.buffer, &msg));
    
    stream.buffer.position = 0;
    assert(qdp_message_read(&stream.buffer, &read_msg));
    assert(read_msg.payload.size == QDP_MAX_PAYLOAD_SIZE);
}

// Stress test for buffer handling
TEST(buffer_stress) {
    uint8_t buffer[QDP_MAX_BUFFER_CAPACITY];
    qdp_stream_t stream = { .buffer = {0}, .encode_buf = {0} };
    stream.buffer = qdp_buffer_create(buffer, sizeof(buffer));
    
    // Write multiple messages until buffer is nearly full
    qdp_message_t msg = {0};
    strcpy(msg.header.topic, "test/stress");
    msg.header.topic_len = strlen(msg.header.topic);
    msg.payload.size = 100;  // reasonable size for stress test
    memset(msg.payload.data, 'Y', msg.payload.size);
    msg.header.payload_len = msg.payload.size;
    
    int count = 0;
    while (1) {
        qdp_stream_t temp_stream = { .buffer = {0}, .encode_buf = {0} };
        temp_stream.buffer = qdp_buffer_create(
            buffer + stream.buffer.size,
            sizeof(buffer) - stream.buffer.size
        );
        
        if (!qdp_message_write(&temp_stream.buffer, &msg)) {
            break;
        }
        
        // Copy the written message to main stream
        memcpy(buffer + stream.buffer.size, temp_stream.buffer.data, temp_stream.buffer.size);
        stream.buffer.size += temp_stream.buffer.size;
        count++;
    }
    
    assert(count > 0);  // Ensure we wrote at least one message
    
    // Read all messages back
    stream.buffer.position = 0;
    int read_count = 0;
    qdp_message_t read_msg;
    
    while (stream.buffer.position < stream.buffer.size) {
        memset(&read_msg, 0, sizeof(read_msg));
        if (!qdp_message_read(&stream.buffer, &read_msg)) {
            break;
        }
        assert(read_msg.payload.size == 100);
        assert(memcmp(read_msg.payload.data, msg.payload.data, msg.payload.size) == 0);
        read_count++;
    }
    
    assert(count == read_count);
}

// Negative test cases
TEST(negative_cases) {
    // Test invalid base64
    uint8_t invalid_base64[] = "!@#$";
    uint8_t output[128];
    size_t output_len = sizeof(output);
    assert(!qdp_base64_decode(invalid_base64, 4, output, &output_len));
    
    // Test buffer overflow prevention
    qdp_message_t msg = {0};
    strcpy(msg.header.topic, "test/overflow");
    msg.header.topic_len = strlen(msg.header.topic);
    msg.payload.size = QDP_MAX_PAYLOAD_SIZE + 1;
    
    uint8_t small_buf[64];
    qdp_stream_t stream = { .buffer = {0}, .encode_buf = {0} };
    stream.buffer = qdp_buffer_create(small_buf, sizeof(small_buf));
    
    assert(!qdp_message_write(&stream.buffer, &msg));
}

// Move these to file scope
static int callback_counter = 0;
static char last_received_topic[QDP_MAX_TOPIC_LENGTH] = {0};

static void advanced_test_callback(const qdp_message_t* msg, void* ctx) {
    (void)ctx;
    callback_counter++;
    strncpy(last_received_topic, msg->header.topic, QDP_MAX_TOPIC_LENGTH-1);
}

TEST(protocol_advanced) {
    qdp_t* qdp = qdp_new();
    uint8_t mock_buf[1024 * 4];
    qdp_buffer_t mock_transport = qdp_buffer_create(mock_buf, sizeof(mock_buf));
    
    callback_counter = 0;
    memset(last_received_topic, 0, sizeof(last_received_topic));
    
    qdp_set_transport(qdp, mock_send, mock_recv, &mock_transport);
    
    // Set up subscriptions
    assert(qdp_subscribe(qdp, "sensor/+/temp", advanced_test_callback, NULL));
    assert(qdp_subscribe(qdp, "control/#", advanced_test_callback, NULL));
    
    // Test first message - publish and process immediately
    mock_transport.size = 0;
    assert(qdp_publish_string(qdp, "sensor/1/temp", "25.5") == 0);
    assert(qdp_process(qdp) == 0);
    assert(callback_counter == 1);
    assert(strcmp(last_received_topic, "sensor/1/temp") == 0);
    
    // Test second message
    mock_transport.size = 0;
    assert(qdp_publish_string(qdp, "control/pump/on", "1") == 0);
    assert(qdp_process(qdp) == 0);
    assert(callback_counter == 2);
    assert(strcmp(last_received_topic, "control/pump/on") == 0);
    
    // Test unsubscribe
    qdp_unsubscribe(qdp, "sensor/+/temp");
    mock_transport.size = 0;
    assert(qdp_publish_string(qdp, "sensor/1/temp", "26.5") == 0);
    assert(qdp_process(qdp) == 0);
    assert(callback_counter == 2); // Should not increase
    
    qdp_free(qdp);
}

// Topic pattern matching tests
TEST(topic_matching) {
    // Exact matches
    assert(qdp_topic_matches("a/b/c", "a/b/c"));
    assert(!qdp_topic_matches("a/b/c", "a/b/d"));
    
    // Single-level wildcard
    assert(qdp_topic_matches("a/+/c", "a/b/c"));
    assert(qdp_topic_matches("a/+/+", "a/b/c"));
    assert(!qdp_topic_matches("a/+/c", "a/b/d"));
    assert(!qdp_topic_matches("a/+/c", "a/b/c/d"));
    
    // Multi-level wildcard
    assert(qdp_topic_matches("a/#", "a/b/c"));
    assert(qdp_topic_matches("a/b/#", "a/b/c/d"));
    assert(!qdp_topic_matches("a/#/c", "a/b/c")); // Invalid pattern
    
    // Mixed wildcards
    assert(qdp_topic_matches("a/+/+/#", "a/b/c/d/e"));
    assert(!qdp_topic_matches("a/+/+/#", "a/b"));
    
    // Edge cases
    assert(!qdp_topic_matches("", "a/b/c"));
    assert(!qdp_topic_matches("a/b/c", ""));
    assert(qdp_topic_matches("#", "a/b/c"));
    assert(qdp_topic_matches("+", "a"));
}

#if defined(__linux__) && defined(__ELF__)
__asm__(".section .note.GNU-stack,\"\",@progbits");
#endif

int main(void) {
    RUN_TEST(buffer_operations);
    RUN_TEST(base64_codec);
    RUN_TEST(message_operations);
    RUN_TEST(protocol_pubsub);
    RUN_TEST(edge_cases);
    RUN_TEST(buffer_stress);
    RUN_TEST(negative_cases);
    RUN_TEST(protocol_advanced);
    RUN_TEST(topic_matching);
    
    printf("All tests passed!\n");
    return 0;
}