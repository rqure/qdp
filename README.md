# QDP (Qureshi Device Protocol) Documentation

## Overview
QDP, or Qureshi Device Protocol, is a secure, lightweight protocol for communication in distributed device environments. It’s designed for embedded systems, SCADA environments, and IoT devices, making it suitable for command exchange, telemetry data collection, and reliable remote operations.

## QDP Message Structure
Each QDP message has a compact, consistent structure divided into three parts:

```
[HEADER] [PAYLOAD] [CHECKSUM/CRC]
```

- **HEADER:** Identifies sender, receiver, message type, and correlation ID.
- **PAYLOAD:** Contains data specific to the message type.
- **CHECKSUM/CRC:** Ensures data integrity.

### 1. Header
The header defines the origin, destination, type, and tracking info for each message. This simple structure ensures accurate routing and efficient parsing.

**Header Format:**
```
HEADER := [FROM] [TO] [PAYLOAD_TYPE] [CORRELATION_ID]
```

| Field           | Type    | Description                                                  |
|-----------------|---------|--------------------------------------------------------------|
| `FROM`          | uint32  | Unique ID of the message's source.                           |
| `TO`            | uint32  | Unique ID of the target device (0 for broadcast messages).   |
| `PAYLOAD_TYPE`  | uint32  | Type of payload (e.g., TELEMETRY_EVENT, COMMAND_REQUEST).    |
| `CORRELATION_ID`| uint32  | Used for tracking request-response pairs asynchronously.     |

**Example Header:**  
If device `0x0001` sends a telemetry request to device `0x0002`:
```
Header Example: [0x0001] [0x0002] [0x0002] [0xABCD]
```

### 2. Payload
The payload contains message-specific data, structured based on `PAYLOAD_TYPE`. QDP’s modular design supports diverse data requirements across devices.

#### Data Types in Payload
Each data unit within the payload has a defined type, size, and content, providing flexibility for different device capabilities.

| Type | Abbreviation | Description                             | Size    |
|------|--------------|-----------------------------------------|---------|
| Null | `N`          | 32-bit signed integer                   | 0 bytes |
| Int  | `I`          | 32-bit signed integer                   | 4 bytes |
| UInt | `UI`         | 32-bit unsigned integer                 | 4 bytes |
| Float| `F`          | 32-bit floating-point                   | 4 bytes |
| String | `S`        | Variable-length, preceded by length     | 4 + n bytes |
| Array | `A`         | Array of data, with prefixed length     | 4 + n bytes |

#### Example: String Payload for "TEMP"
```
Payload Example: [S] [4] ["T", "E", "M", "P"]
```

### Payload Types
1. **TELEMETRY_EVENT**  
   Reports updated telemetry data when a device condition changes.  
   Example: Temperature reading of 25.5°C as a float.
   ```
   TELEMETRY_EVENT PAYLOAD := [F] [4] [0x41, 0xC8, 0x00, 0x00]
   ```

2. **TELEMETRY_REQUEST**  
   Requests current telemetry data from a device. Payload is empty.
   ```
   TELEMETRY_REQUEST PAYLOAD := [N] [0] []
   ```

3. **TELEMETRY_RESPONSE**  
   Responds to `TELEMETRY_REQUEST` with requested data.
   ```
   TELEMETRY_RESPONSE PAYLOAD := [UI] [4] [0x00, 0x00, 0x00, 0x41]
   ```

4. **COMMAND_REQUEST**  
   Sends a command to the device with optional parameters.  
   Example: Command to set temperature threshold to 22.5°C.
   ```
   COMMAND_REQUEST PAYLOAD := [F] [4] [0x41, 0x38, 0x00, 0x00]
   ```

5. **COMMAND_RESPONSE**  
   Confirms execution of `COMMAND_REQUEST` with success/failure and result.
   ```
   COMMAND_RESPONSE PAYLOAD := [F] [4] [0x41, 0x38, 0x00, 0x00]
   ```

6. **DEVICE_ID_REQUEST**  
   Requests a list of device IDs managed by a network device. Payload is empty.
   ```
   DEVICE_ID_REQUEST PAYLOAD := [N] [0] []
   ```

7. **DEVICE_ID_RESPONSE**  
   Responds to `DEVICE_ID_REQUEST` with a list of device IDs.
   ```
   DEVICE_ID_RESPONSE PAYLOAD := [A] [24] [UI] [4] [0x00, 0x00, 0x00, 0x01] [UI] [4] [0x00, 0x00, 0x00, 0x02]
   ```

8. **ERROR_RESPONSE**  
   Reports failure of a request with error details.
   ```
   ERROR_RESPONSE PAYLOAD := [S] [13] [0x45, 0x72, 0x72, 0x6F, 0x72, 0x20, 0x4D, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65]
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
