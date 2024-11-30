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
} tcp_context_t;

// TCP transport handlers
static inline int tcp_send(qdp_buffer_t* buf, void* ctx) {
    tcp_context_t* tcp = (tcp_context_t*)ctx;
    ssize_t sent = send(tcp->sock, buf->data, buf->size, 0);
    return sent >= 0 && (size_t)sent == buf->size ? 0 : -1;
}

static inline int tcp_recv(qdp_buffer_t* buf, void* ctx) {
    tcp_context_t* tcp = (tcp_context_t*)ctx;
    ssize_t received = recv(tcp->sock, buf->data, buf->capacity, MSG_DONTWAIT);
    if (received > 0) {
        buf->size = received;
        return 0;
    } else if (received == -1 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
        buf->size = 0;
        return 0;
    }
    return -1;
}