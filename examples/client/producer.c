#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include "tcp_transport.h"

#define MAX_ADDR_LEN 256

int main(int argc, char *argv[]) {
    if (argc != 4) {
        fprintf(stderr, "Usage: %s <host:port> <topic> <message>\n", argv[0]);
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

    // Setup QDP and publish message
    qdp_t* qdp = qdp_new();
    if (!qdp) {
        fprintf(stderr, "Failed to create QDP context\n");
        close(tcp_ctx.sock);
        return 1;
    }

    qdp_set_transport(qdp, tcp_send, tcp_recv, &tcp_ctx);
    
    if (qdp_publish_string(qdp, argv[2], argv[3]) < 0) {
        fprintf(stderr, "Failed to publish message\n");
        qdp_free(qdp);
        close(tcp_ctx.sock);
        return 1;
    }

    printf("Published to '%s': %s\n", argv[2], argv[3]);

    qdp_free(qdp);
    close(tcp_ctx.sock);
    return 0;
}