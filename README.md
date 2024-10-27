# QDP (Qureshi Device Protocol)

## Purpose of QDP
QDP (Qureshi Device Protocol) is designed to facilitate secure, lightweight communication between devices in a distributed environment. It is optimized for embedded systems, SCADA environments, and IoT devices, enabling them to send commands, receive telemetry data, and ensure reliable operations for remote devices.

## Message Structure
QDP messages are structured to be both compact and versatile, supporting various functions such as command execution, telemetry reporting, and error handling. Messages are composed of three main sections:

```
[HEADER] [PAYLOAD] [CHECKSUM/CRC]
```

### Device Abstraction

In this specification, the term device refers abstractly to any endpoint within the network, such as sensors, controllers, or intermediary systems (like microcontrollers or embedded processors). The device could be an MCU, a specific sensor, or any other node designated by the implementer. This protocol is flexible in allowing the device’s role to act as either a standalone node or a network “router” for various connected peripherals.

### Header
The header defines the message’s source, destination, type, and tracking information, which allows devices to route, interpret, and respond to messages accurately. It uses a fixed format to maintain simplicity and facilitate efficient parsing.

```
HEADER := [FROM] [TO] [PAYLOAD_TYPE] [CORRELATION_ID]
```

#### Header Fields:

    FROM: A uint32 (4 bytes) representing the unique ID of the device originating the message.
    TO: A uint32 (4 bytes) indicating the unique ID of the destination device.
        A value of 0 indicates that the message is broadcast and not directed to a specific device.
    PAYLOAD_TYPE: A uint32 (4 bytes) defining the type of payload contained within the message. Supported types are:
        TELEMETRY_EVENT: Used to report updated telemetry data from a device. Contains the telemetry data in the payload.
        TELEMETRY_REQUEST: A request for telemetry data from another device. Contains an empty payload (0 bytes).
        TELEMETRY_RESPONSE: A response containing telemetry data requested via a TELEMETRY_REQUEST.
        COMMAND_REQUEST: A request for a device to perform an action or modify its settings. Payload may include parameters for the action.
        COMMAND_RESPONSE: Sent in reply to a COMMAND_REQUEST, indicating success or failure of the requested action. The payload returns the new data value if successful.
        DEVICE_ID_REQUEST: A request for a list of device IDs managed by a router or gateway device.
        ERROR_RESPONSE: A response indicating failure of a previous request, such as *_REQUEST. Contains an error code and relevant details in the payload.
    CORRELATION_ID: An identifier for tracking request-response pairs, allowing asynchronous correlation between requests and their responses.

#### Example Header
```
Example: 0x0001 0x0002 0x0002 0xABCD
```
In this example, the message is from device `0x0001`, directed to device `0x0002`, with a PAYLOAD_TYPE of `0x0002` (such as a `TELEMETRY_REQUEST`), and a `CORRELATION_ID` of `0xABCD` for asynchronous tracking.

### Payload
The payload’s structure varies according to the PAYLOAD_TYPE specified in the header. It contains the data necessary for the recipient to act on the message, such as telemetry information, command parameters, or error details. The payload structure is modular, allowing each payload to be built using one or more data units.

**Data Unit Structure:** Each data unit within the payload contains a defined type, size, and data. This provides flexibility in handling different data types across devices with varying capabilities.

#### Data Fields:

    DATA_TYPE: An enumeration representing the type of data contained in the unit. Supported types include:
        I: 32-bit signed integer (int32), 4 bytes.
        UI: 32-bit unsigned integer (uint32), 4 bytes.
        F: 32-bit floating-point (float32), 4 bytes.
        S: String. Accompanied by a string length (uint32) and the string characters (1 byte per character).
        A: Array of data. Accompanied by an array length (uint32) followed by each data item, structured according to its DATA_TYPE.

For example, a string payload of "TEMP" could be represented as:

```
DATA := [S] [4] ['T', 'E', 'M', 'P']
```

Each DATA block within the payload may represent a unique piece of telemetry data, a command parameter, or an error description, depending on the PAYLOAD_TYPE.

#### 1. **TELEMETRY_EVENT**
Used by a device to report updated telemetry data when certain conditions are met (e.g., temperature change).

```
TELEMETRY_EVENT PAYLOAD := [DATA_TYPE] [DATA_SIZE_IN_BYTES] [DATA_BYTES...]
```

- **`DATA_TYPE`**: Specifies the type of telemetry data (e.g., `I` for int, `F` for float).
- **`DATA_SIZE_IN_BYTES`**: Length of the telemetry data in bytes.
- **`DATA_BYTES`**: Actual telemetry data (e.g., temperature value, humidity level).

**Example**: A temperature report of 25.5°C as a float.
```
TELEMETRY_EVENT PAYLOAD := [F] [4] [0x41, 0xC8, 0x00, 0x00]
```

---

#### 2. **TELEMETRY_REQUEST**
A request sent to a device to retrieve its current telemetry data. The payload is empty for this type, as it only indicates a request.

```
TELEMETRY_REQUEST PAYLOAD := []
```

**Example**: The payload section remains 0 bytes.

---

#### 3. **TELEMETRY_RESPONSE**
This is a response to a `TELEMETRY_REQUEST`, where the payload contains the requested telemetry data.

```
TELEMETRY_RESPONSE PAYLOAD := [DATA_TYPE] [DATA_SIZE_IN_BYTES] [DATA_BYTES...]
```

- **`DATA_TYPE`**: The type of data being reported (e.g., `F` for float).
- **`DATA_SIZE_IN_BYTES`**: Length of the telemetry data.
- **`DATA_BYTES`**: The actual telemetry data value.

**Example**: A response providing humidity as `65%` in an unsigned integer format.
```
TELEMETRY_RESPONSE PAYLOAD := [UI] [4] [0x00, 0x00, 0x00, 0x41]
```

---

#### 4. **COMMAND_REQUEST**
Used to send a command to a device, which may require action based on parameters provided in the payload.

```
COMMAND_REQUEST PAYLOAD := [COMMAND_ID] [DATA_TYPE] [DATA_SIZE_IN_BYTES] [DATA_BYTES...]
```

- **`COMMAND_ID`**: A unique identifier for the command (e.g., `0x01` for `Set Temperature`).
- **`DATA_TYPE`**: Specifies the type of parameter data (e.g., `F` for float).
- **`DATA_SIZE_IN_BYTES`**: Length of the command parameter.
- **`DATA_BYTES`**: Actual parameter value.

**Example**: A command to set the threshold temperature to `22.5°C`.
```
COMMAND_REQUEST PAYLOAD := [0x01] [F] [4] [0x41, 0x38, 0x00, 0x00]
```

---

#### 5. **COMMAND_RESPONSE**
Sent in response to a `COMMAND_REQUEST`, confirming success or failure. If successful, it returns the newly set data value.

```
COMMAND_RESPONSE PAYLOAD := [STATUS_CODE] [DATA_TYPE] [DATA_SIZE_IN_BYTES] [DATA_BYTES...]
```

- **`STATUS_CODE`**: An integer indicating success (`0x00`) or error (`0x01`).
- **`DATA_TYPE`**: The type of data returned (matches `COMMAND_REQUEST` if successful).
- **`DATA_SIZE_IN_BYTES`**: Length of the returned data.
- **`DATA_BYTES`**: The value returned by the command (if successful).

**Example**: Successful response to setting temperature threshold to `22.5°C`.
```
COMMAND_RESPONSE PAYLOAD := [0x00] [F] [4] [0x41, 0x38, 0x00, 0x00]
```

---

#### 6. **DEVICE_ID_REQUEST**
A request to retrieve a list of device IDs known to a router or network coordinator. This payload is empty as it’s simply a request.

```
DEVICE_ID_REQUEST PAYLOAD := []
```

**Example**: The payload section remains 0 bytes.

---

#### 7. **DEVICE_ID_RESPONSE**
Response to a `DEVICE_ID_REQUEST`, containing a list of device IDs.

```
DEVICE_ID_RESPONSE PAYLOAD := [NUM_DEVICES] [DEVICE_ID_LIST...]
```

- **`NUM_DEVICES`**: Number of device IDs returned (`uint32`).
- **`DEVICE_ID_LIST`**: Series of `uint32` device IDs.

**Example**: Response with two device IDs: `0x0001` and `0x0002`.
```
DEVICE_ID_RESPONSE PAYLOAD := [0x02] [0x00000001] [0x00000002]
```

---

#### 8. **ERROR_RESPONSE**
Used to indicate a failure in response to any `*_REQUEST`, containing an error code and, optionally, additional details.

```
ERROR_RESPONSE PAYLOAD := [ERROR_CODE] [DETAIL_LENGTH] [DETAIL_BYTES...]
```

- **`ERROR_CODE`**: A code identifying the type of error (e.g., `0x01` for `Invalid Command`).
- **`DETAIL_LENGTH`**: Length of the error detail message.
- **`DETAIL_BYTES`**: ASCII string or binary data explaining the error.

**Example**: Error response with code `0x01` (`Invalid Command`) and a detail message “Command not recognized.”
```
ERROR_RESPONSE PAYLOAD := [0x01] [0x14] ["Command not recognized"]
```

---

This structure ensures that each `PAYLOAD_TYPE` in QDP has a clearly defined format, making it easier for devices to parse, interpret, and act upon received messages accurately.

### Checksum/CRC
The checksum in QDP uses a 32-bit CRC (Cyclic Redundancy Check) to ensure data integrity, helping detect errors in transmission. QDP uses the CRC32 algorithm with polynomial 0xEDB88320, which is lightweight and suitable for embedded systems and IoT devices.

#### Calculation Process

    Algorithm: CRC32 with the polynomial 0xEDB88320.
    Data Scope: The CRC is calculated over both the HEADER and PAYLOAD sections of the message.
    Final XOR: After processing the data, the CRC is XORed with 0xFFFFFFFF to finalize the value.

The CRC calculation steps:

    Initialize a CRC table using the polynomial.
    Iterate over each byte of [HEADER] [PAYLOAD], updating the CRC value with a table lookup for each byte.
    XOR the final CRC value with 0xFFFFFFFF.

#### Example Code for CRC Calculation

The CRC table-based calculation allows fast processing and is easily adaptable to various programming languages.

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

#### Verification Process

On the receiving side, the CRC is verified to ensure data integrity:

    Extract the Received CRC: The received CRC (last 4 bytes of the message) is separated from [HEADER] [PAYLOAD].
    Calculate CRC on Data: Use the same CRC32 function to calculate the CRC over [HEADER] [PAYLOAD], excluding the received CRC.
    Compare CRCs: Compare the calculated CRC to the received CRC. If they match, the message is valid. If they don’t, it indicates an error in transmission.

Example Code for CRC Verification
```c
bool verify_crc32(const uint8_t *message, size_t length, uint32_t received_crc) {
    // Calculate CRC on the message data (excluding received CRC)
    uint32_t calculated_crc = calculate_crc32(message, length);
    return (calculated_crc == received_crc);
}
```

#### Transmission Guidelines

    Sender: Calculate CRC on [HEADER] [PAYLOAD], then append the 4-byte CRC to form [HEADER] [PAYLOAD] [CRC].
    Receiver: Separate the received CRC from [HEADER] [PAYLOAD], calculate the CRC on [HEADER] [PAYLOAD], and verify by comparison.

This CRC-based approach provides efficient, reliable error checking suitable for QDP's lightweight, embedded communication environment.
