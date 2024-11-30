#pragma once

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <errno.h>
#include "../../lib/c/qdp.h"

#define MAX_ADDR_LEN 256

typedef struct {
    int sock;
    uint8_t partial_buf[QDP_MAX_BUFFER_CAPACITY];  // Buffer for partial messages
    size_t partial_size;                           // Current bytes in partial buffer
} tcp_context_t;

// TCP transport handlers
static inline int tcp_send(qdp_buffer_t* buf, void* ctx) {
    tcp_context_t* tcp = (tcp_context_t*)ctx;
    ssize_t sent = send(tcp->sock, buf->data, buf->size, 0);
    return sent >= 0 && (size_t)sent == buf->size ? 0 : -1;
}

static inline int tcp_recv(qdp_buffer_t* buf, void* ctx) {
    tcp_context_t* tcp = (tcp_context_t*)ctx;
    
    // First, copy any partial data we have
    if (tcp->partial_size > 0) {
        memcpy(buf->data, tcp->partial_buf, tcp->partial_size);
        buf->size = tcp->partial_size;
    }

    // Try to read remaining data
    ssize_t received = recv(tcp->sock, 
                          buf->data + buf->size,
                          buf->capacity - buf->size, 
                          MSG_DONTWAIT);
                          
    if (received > 0) {
        buf->size += received;
        tcp->partial_size = buf->size;  // Store all data as partial for next read
        memcpy(tcp->partial_buf, buf->data, buf->size);
        return 0;
    } else if (received == -1 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
        // No new data, but we might have partial data
        return 0;
    }
    
    // Error or connection closed
    if (received == 0 || received == -1) {
        tcp->partial_size = 0;  // Clear partial buffer on error
    }
    return received == 0 ? -1 : (errno == EINTR ? 0 : -1);
}