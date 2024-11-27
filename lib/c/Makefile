CC = gcc
CFLAGS = -Wall -Wextra -Werror -g -I. -fno-stack-protector
LDFLAGS = 

SRC = qdp.c test_qdp.c
OBJ = $(SRC:.c=.o)
TARGET = test_qdp

.PHONY: all clean test

all: $(TARGET)

$(TARGET): $(OBJ)
	$(CC) $(OBJ) -o $(TARGET) $(LDFLAGS)

%.o: %.c
	$(CC) $(CFLAGS) -c $< -o $@

test: $(TARGET)
	./$(TARGET)

clean:
	rm -f $(OBJ) $(TARGET)