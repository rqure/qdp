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
    printf("%s: %s\n", qdp_get_topic(msg), qdp_get_string(msg));
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

    if (connect(tcp_ctx.sock, (struct sockaddr*)&server_addr, sizeof(server_addr)) < 0) {
        perror("Connection failed");
        close(tcp_ctx.sock);
        return 1;
    }

    // Setup QDP
    qdp_t* qdp = qdp_new();
    if (!qdp) {
        fprintf(stderr, "Failed to create QDP context\n");
        close(tcp_ctx.sock);
        return 1;
    }

    qdp_set_transport(qdp, tcp_send, tcp_recv, &tcp_ctx);
    qdp_subscribe(qdp, argv[2], message_handler, NULL);

    // Setup signal handler for graceful shutdown
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);

    printf("Listening for messages on '%s'...\n", argv[2]);

    // Message loop
    while (running) {
        if (qdp_process(qdp) < 0) {
            fprintf(stderr, "Connection lost\n");
            break;
        }
        usleep(1000); // Small sleep to prevent CPU spinning
    }

    printf("\nShutting down...\n");
    qdp_free(qdp);
    close(tcp_ctx.sock);
    return 0;
}