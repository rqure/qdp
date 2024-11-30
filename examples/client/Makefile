CC = gcc
CFLAGS = -Wall -Wextra -I../../lib/c
LDFLAGS = 

TARGETS = qdp_consumer qdp_producer
QDP_LIB = ../../lib/c/qdp.c

all: $(TARGETS)

qdp_consumer: consumer.c $(QDP_LIB)
	$(CC) $(CFLAGS) $^ -o $@ $(LDFLAGS)

qdp_producer: producer.c $(QDP_LIB)
	$(CC) $(CFLAGS) $^ -o $@ $(LDFLAGS)

clean:
	rm -f $(TARGETS) *.o

.PHONY: all clean