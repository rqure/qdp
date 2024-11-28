# QDP (Qureshi Device Protocol) Documentation

## Overview
QDP, or Qureshi Device Protocol, is a secure, lightweight protocol for communication in distributed device environments. It’s designed for embedded systems, SCADA environments, and IoT devices, making it suitable for command exchange, telemetry data collection, and reliable remote operations.

## QDP Message Structure
Each QDP message has a compact, consistent structure divided into three parts:

```
[HEADER] [PAYLOAD] [CHECKSUM/CRC]
```

- **HEADER:** Identifies the topic length, topic, and payload length.
- **PAYLOAD:** Contains data, usually in string form.
- **CHECKSUM/CRC:** Ensures data integrity.

### 1. Header
The header defines the length of the topic, the topic itself, and the length of the payload. This simple structure ensures accurate routing and efficient parsing.

**Header Format:**
```
HEADER := [TOPIC_LENGTH] [PAYLOAD_LENGTH] [TOPIC]
```

| Field           | Type    | Description                                                  |
|-----------------|---------|--------------------------------------------------------------|
| `TOPIC_LENGTH`  | uint32  | Length of the topic string in bytes.                         |
| `PAYLOAD_LENGTH`| uint32  | Length of the payload in bytes.                              |
| `TOPIC`         | string  | Topic of the message.                                        |

**Example Header:**  
If the topic is `temperature` and the payload length is `4` bytes:
```
Header Example: [11] [4] [temperature]
```

### 2. Payload
The payload contains message-specific data, structured based on `TOPIC`. QDP’s modular design supports diverse data requirements across devices.

#### Example: String Payload for "TEMP"
```
Payload Example: ["T", "E", "M", "P"]
```

### 3. Checksum/CRC
To maintain data integrity, QDP uses a 32-bit CRC (Cyclic Redundancy Check), calculated over the `HEADER` and `PAYLOAD` sections. The protocol uses CRC32 with polynomial `0xEDB88320`.

#### CRC Calculation
1. Initialize the CRC table using the polynomial.
2. Process each byte in `[HEADER] [PAYLOAD]` using the CRC table.
3. XOR the final CRC with `0xFFFFFFFF`.

**Example C Code for CRC Calculation:**
```c
#define POLYNOMIAL 0xEDB88320
uint32_t crc32_table[256];

void generate_crc32_table() {
    for (int i = 0; i < 256; i++) {
        uint32_t crc = i;
        for (int j = 0; j < 8; j++) {
            crc = (crc & 1) ? (crc >> 1) ^ POLYNOMIAL : crc >> 1;
        }
        crc32_table[i] = crc;
    }
}

uint32_t calculate_crc32(const uint8_t *data, size_t length) {
    uint32_t crc = 0xFFFFFFFF;
    for (size_t i = 0; i < length; i++) {
        crc = (crc >> 8) ^ crc32_table[(crc ^ data[i]) & 0xFF];
    }
    return crc ^ 0xFFFFFFFF;
}
```

#### CRC Verification
1. Extract received CRC.
2. Calculate CRC over `[HEADER] [PAYLOAD]`.
3. Verify that calculated CRC matches the received CRC.

**Example Verification Code:**
```c
bool verify_crc32(const uint8_t *message, size_t length, uint32_t received_crc) {
    uint32_t calculated_crc = calculate_crc32(message, length);
    return (calculated_crc == received_crc);
}
```

## Transmission Guidelines
- **Sender:** Calculate and append CRC to `[HEADER] [PAYLOAD]`.
- **Receiver:** Separate received CRC, calculate on `[HEADER] [PAYLOAD]`, then verify.
