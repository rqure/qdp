#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include "tcp_transport.h"

static volatile int running = 1;

static void signal_handler(int sig) {
    (void)sig;
    running = 0;
}

static void message_handler(const qdp_message_t* msg, void* ctx) {
    (void)ctx;
    size_t len;
    const char* payload = qdp_get_string(msg);
    const void* bytes = qdp_get_bytes(msg, &len);
    printf("Received message:\n");
    printf("  Topic: %s\n", qdp_get_topic(msg));
    printf("  Payload length: %zu\n", len);
    if (payload) {
        printf("  Content: %s\n", payload);
    }
}

int main(int argc, char *argv[]) {
    if (argc != 3) {
        fprintf(stderr, "Usage: %s <host:port> <topic>\n", argv[0]);
        return 1;
    }

    // Parse host:port
    char host[MAX_ADDR_LEN];
    int port;
    if (sscanf(argv[1], "%[^:]:%d", host, &port) != 2) {
        fprintf(stderr, "Invalid address format. Use: host:port\n");
        return 1;
    }

    // Setup TCP connection
    tcp_context_t tcp_ctx = {0};
    struct sockaddr_in server_addr = {
        .sin_family = AF_INET,
        .sin_port = htons(port),
    };

    if (inet_pton(AF_INET, host, &server_addr.sin_addr) <= 0) {
        fprintf(stderr, "Invalid address\n");
        return 1;
    }

    tcp_ctx.sock = socket(AF_INET, SOCK_STREAM, 0);
    if (tcp_ctx.sock < 0) {
        perror("Socket creation failed");
        return 1;
    }

    printf("Connecting to %s:%d...\n", host, port);
    if (connect(tcp_ctx.sock, (struct sockaddr*)&server_addr, sizeof(server_addr)) < 0) {
        perror("Connection failed");
        close(tcp_ctx.sock);
        return 1;
    }
    printf("Connected successfully\n");

    // Setup QDP
    qdp_t* qdp = qdp_new();
    if (!qdp) {
        fprintf(stderr, "Failed to create QDP context\n");
        close(tcp_ctx.sock);
        return 1;
    }

    qdp_set_transport(qdp, tcp_send, tcp_recv, &tcp_ctx);
    if (!qdp_subscribe(qdp, argv[2], message_handler, NULL)) {
        fprintf(stderr, "Failed to subscribe to topic: %s\n", argv[2]);
        qdp_free(qdp);
        close(tcp_ctx.sock);
        return 1;
    }
    printf("Subscribed to topic: %s\n", argv[2]);

    // Setup signal handler for graceful shutdown
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);

    printf("Listening for messages on '%s'...\n", argv[2]);

    // Message loop
    int idle_count = 0;
    while (running) {
        int result = qdp_process(qdp);
        if (result < 0) {
            fprintf(stderr, "Error processing messages: %d\n", result);
            break;
        }
        
        idle_count++;
        if (idle_count >= 1000) {  // Print status every ~1 second
            printf("Waiting for messages...\n");
            idle_count = 0;
        }
        
        usleep(1000); // Small sleep to prevent CPU spinning
    }

    printf("\nShutting down...\n");
    qdp_free(qdp);
    close(tcp_ctx.sock);
    return 0;
}