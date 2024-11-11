package main

import (
	"encoding/binary"
	"fmt"
	"math"

	qdb "github.com/rqure/qdb/src"
)

type QdpPayloadType uint32

const (
	PAYLOAD_TYPE_TELEMETRY_EVENT = QdpPayloadType(iota + 1)
	PAYLOAD_TYPE_TELEMETRY_REQUEST
	PAYLOAD_TYPE_TELEMETRY_RESPONSE
	PAYLOAD_TYPE_COMMAND_REQUEST
	PAYLOAD_TYPE_COMMAND_RESPONSE
	PAYLOAD_TYPE_DEVICE_ID_REQUEST
	PAYLOAD_TYPE_DEVICE_ID_RESPONSE
	PAYLOAD_TYPE_ERROR_RESPONSE
)

func (t QdpPayloadType) String() string {
	switch t {
	case PAYLOAD_TYPE_TELEMETRY_EVENT:
		return "telemetry-event"
	case PAYLOAD_TYPE_TELEMETRY_REQUEST:
		return "telemetry-request"
	case PAYLOAD_TYPE_TELEMETRY_RESPONSE:
		return "telemetry-response"
	case PAYLOAD_TYPE_COMMAND_REQUEST:
		return "command-request"
	case PAYLOAD_TYPE_COMMAND_RESPONSE:
		return "command-response"
	case PAYLOAD_TYPE_DEVICE_ID_REQUEST:
		return "device-id-request"
	case PAYLOAD_TYPE_DEVICE_ID_RESPONSE:
		return "device-id-response"
	case PAYLOAD_TYPE_ERROR_RESPONSE:
		return "error-response"
	default:
		return "unknown"
	}
}

type QdpDataType uint32

const (
	DATA_TYPE_NULL = QdpDataType(iota)
	DATA_TYPE_INT
	DATA_TYPE_UINT
	DATA_TYPE_FLOAT
	DATA_TYPE_STRING
	DATA_TYPE_ARRAY
)

func (t QdpDataType) String() string {
	switch t {
	case DATA_TYPE_NULL:
		return "N"
	case DATA_TYPE_INT:
		return "I"
	case DATA_TYPE_UINT:
		return "UI"
	case DATA_TYPE_FLOAT:
		return "F"
	case DATA_TYPE_STRING:
		return "S"
	case DATA_TYPE_ARRAY:
		return "A"
	default:
		return "UK"
	}
}

type QdpHeader struct {
	From          uint32
	To            uint32
	PayloadType   QdpPayloadType
	CorrelationId uint32
}

func (h *QdpHeader) String() string {
	return fmt.Sprintf("H{F: %d, T: %d, PT: %s, C: %d}", h.From, h.To, h.PayloadType, h.CorrelationId)
}

type QdpPayload struct {
	DataType QdpDataType
	Size     uint32
	Data     []byte
}

func (p *QdpPayload) String() string {
	if p.DataType == DATA_TYPE_INT {
		return fmt.Sprintf("P{DT: %s, S: %d, D: %d}", p.DataType, p.Size, p.GetInt())
	} else if p.DataType == DATA_TYPE_UINT {
		return fmt.Sprintf("P{DT: %s, S: %d, D: %d}", p.DataType, p.Size, p.GetUint())
	} else if p.DataType == DATA_TYPE_FLOAT {
		return fmt.Sprintf("P{DT: %s, S: %d, D: %f}", p.DataType, p.Size, p.GetFloat())
	} else if p.DataType == DATA_TYPE_STRING {
		return fmt.Sprintf("P{DT: %s, S: %d, D: %s}", p.DataType, p.Size, p.GetString())
	} else if p.DataType == DATA_TYPE_ARRAY {
		return fmt.Sprintf("P{DT: %s, S: %d, D: %v}", p.DataType, p.Size, p.GetArray())
	} else if p.DataType == DATA_TYPE_NULL {
		return fmt.Sprintf("P{DT: %s, S: %d, D: nil}", p.DataType, p.Size)
	} else {
		return fmt.Sprintf("P{DT: %s, S: %d, D: ?%v?}", p.DataType, p.Size, p.Data)
	}
}

type QdpMessageValidity uint32

const (
	MESSAGE_VALIDITY_INCOMPLETE = QdpMessageValidity(iota)
	MESSAGE_VALIDITY_INVALID
	MESSAGE_VALIDITY_VALID
)

type QdpMessage struct {
	Header   QdpHeader
	Payload  QdpPayload
	Checksum uint32
	Validity QdpMessageValidity
}

type IQdpTransport interface {
	Send(m *QdpMessage)
	Recv() *QdpMessage
}

func (p *QdpPayload) SetInt(v int) {
	p.DataType = DATA_TYPE_INT
	p.Size = 4
	p.Data = make([]byte, 4)
	binary.LittleEndian.PutUint32(p.Data, uint32(v))
}

func (p *QdpPayload) GetInt() int {
	return int(binary.LittleEndian.Uint32(p.Data))
}

func (p *QdpPayload) SetUint(v uint) {
	p.DataType = DATA_TYPE_UINT
	p.Size = 4
	p.Data = make([]byte, 4)
	binary.LittleEndian.PutUint32(p.Data, uint32(v))
}

func (p *QdpPayload) GetUint() uint {
	return uint(binary.LittleEndian.Uint32(p.Data))
}

func (p *QdpPayload) SetFloat(v float32) {
	p.DataType = DATA_TYPE_FLOAT
	p.Size = 4
	p.Data = make([]byte, 4)
	binary.LittleEndian.PutUint32(p.Data, math.Float32bits(v))
}

func (p *QdpPayload) GetFloat() float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(p.Data))
}

func (p *QdpPayload) SetString(v string) {
	p.DataType = DATA_TYPE_STRING
	p.Size = uint32(len(v)) + 1
	p.Data = append([]byte(v), 0)
}

func (p *QdpPayload) GetString() string {
	return string(p.Data[:len(p.Data)-1])
}

func (p *QdpPayload) SetArray(a []QdpPayload) {
	p.DataType = DATA_TYPE_ARRAY
	p.Size = 0
	p.Data = make([]byte, p.Size)
	for _, v := range a {
		p.Size += 4 + 4 + v.Size
		dt := make([]byte, 4)
		binary.LittleEndian.PutUint32(dt, uint32(v.DataType))
		sz := make([]byte, 4)
		binary.LittleEndian.PutUint32(sz, v.Size)
		p.Data = append(p.Data, dt...)
		p.Data = append(p.Data, sz...)
		p.Data = append(p.Data, v.Data...)
	}
}

func (p *QdpPayload) GetArray() []QdpPayload {
	a := make([]QdpPayload, 0)
	for i := 0; i < len(p.Data); {
		dt := binary.LittleEndian.Uint32(p.Data[i : i+4])
		i += 4
		sz := binary.LittleEndian.Uint32(p.Data[i : i+4])
		i += 4
		d := make([]byte, sz)
		copy(d, p.Data[i:i+int(sz)])
		i += int(sz)
		a = append(a, QdpPayload{
			DataType: QdpDataType(dt),
			Size:     sz,
			Data:     d,
		})
	}
	return a
}

func (a *QdpPayload) AppendArray(item QdpPayload) {
	a.Size += 4 + 4 + item.Size
	dt := make([]byte, 4)
	binary.LittleEndian.PutUint32(dt, uint32(item.DataType))
	sz := make([]byte, 4)
	binary.LittleEndian.PutUint32(sz, item.Size)
	a.Data = append(a.Data, dt...)
	a.Data = append(a.Data, sz...)
	a.Data = append(a.Data, item.Data...)
}

func (p *QdpPayload) SetNull() {
	p.DataType = DATA_TYPE_NULL
	p.Size = 0
	p.Data = make([]byte, 0)
}

func (p *QdpPayload) ClearArray() {
	p.DataType = DATA_TYPE_ARRAY
	p.Size = 0
	p.Data = make([]byte, 0)
}

func (m *QdpMessage) Complete() {
	m.Checksum = m.CalculateChecksum()
	m.Validity = MESSAGE_VALIDITY_VALID
}

func (m *QdpMessage) FromBytes(b []byte) []byte {
	m.Validity = MESSAGE_VALIDITY_INCOMPLETE

	if len(b) < 24 {
		return b
	}

	m.Header.From = binary.LittleEndian.Uint32(b[0:4])
	m.Header.To = binary.LittleEndian.Uint32(b[4:8])
	m.Header.PayloadType = QdpPayloadType(binary.LittleEndian.Uint32(b[8:12]))
	m.Header.CorrelationId = binary.LittleEndian.Uint32(b[12:16])
	m.Payload.DataType = QdpDataType(binary.LittleEndian.Uint32(b[16:20]))
	m.Payload.Size = binary.LittleEndian.Uint32(b[20:24])

	if len(b) < 24+int(m.Payload.Size)+4 {
		return b
	}

	m.Payload.Data = make([]byte, m.Payload.Size)
	if m.Payload.Size > 0 {
		copy(m.Payload.Data, b[24:24+m.Payload.Size])
	}
	m.Checksum = binary.LittleEndian.Uint32(b[24+m.Payload.Size : 28+m.Payload.Size])

	m.Validity = MESSAGE_VALIDITY_VALID

	calculatedChecksum := m.CalculateChecksum()
	if m.Checksum != calculatedChecksum {
		qdb.Warn("[QdpMessage::FromBytes] Invalid checksum: %d != %d", m.Checksum, calculatedChecksum)
		m.Validity = MESSAGE_VALIDITY_INVALID
	}

	return b[28+m.Payload.Size:]
}

func (m *QdpMessage) ToBytes() []byte {
	b := make([]byte, 0)

	from := make([]byte, 4)
	binary.LittleEndian.PutUint32(from, m.Header.From)
	b = append(b, from...)

	to := make([]byte, 4)
	binary.LittleEndian.PutUint32(to, m.Header.To)
	b = append(b, to...)

	pt := make([]byte, 4)
	binary.LittleEndian.PutUint32(pt, uint32(m.Header.PayloadType))
	b = append(b, pt...)

	cid := make([]byte, 4)
	binary.LittleEndian.PutUint32(cid, m.Header.CorrelationId)
	b = append(b, cid...)

	dt := make([]byte, 4)
	binary.LittleEndian.PutUint32(dt, uint32(m.Payload.DataType))
	b = append(b, dt...)

	sz := make([]byte, 4)
	binary.LittleEndian.PutUint32(sz, m.Payload.Size)
	b = append(b, sz...)

	b = append(b, m.Payload.Data...)

	cs := make([]byte, 4)
	binary.LittleEndian.PutUint32(cs, m.Checksum)
	b = append(b, cs...)

	return b
}

func (m *QdpMessage) CalculateChecksum() uint32 {
	b := m.ToBytes()
	return crc32table.Calculate(b[:len(b)-4])
}

func (m *QdpMessage) String() string {
	return fmt.Sprintf("M{%s, %s, %d}", m.Header.String(), m.Payload.String(), m.Checksum)
}

type Crc32Table [256]uint32

func NewCrc32Table() *Crc32Table {
	t := &Crc32Table{}
	for i := 0; i < 256; i++ {
		crc := uint32(i)
		for j := 0; j < 8; j++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc = crc >> 1
			}
		}
		t[i] = crc
	}
	return t
}

func (t *Crc32Table) Calculate(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc >> 8) ^ t[(crc^uint32(b))&0xFF]
	}
	return crc ^ 0xFFFFFFFF
}

var crc32table *Crc32Table = NewCrc32Table()
