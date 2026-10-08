package ant

import "fmt"

// Message IDs, "ANT Message Protocol and Usage" section 9.5.
const (
	MsgChannelEvent             = 0x40 // channel response or RF event
	MsgUnassignChannel          = 0x41
	MsgAssignChannel            = 0x42
	MsgChannelPeriod            = 0x43
	MsgSearchTimeout            = 0x44
	MsgRFFrequency              = 0x45
	MsgSetNetworkKey            = 0x46
	MsgResetSystem              = 0x4A
	MsgOpenChannel              = 0x4B
	MsgCloseChannel             = 0x4C
	MsgRequest                  = 0x4D
	MsgBroadcastData            = 0x4E
	MsgAcknowledgedData         = 0x4F
	MsgBurstData                = 0x50
	MsgChannelID                = 0x51
	MsgCapabilities             = 0x54
	MsgLowPrioritySearchTimeout = 0x63
	MsgStartup                  = 0x6F
	MsgSerialError              = 0xAE
)

// msgRFEvent is the message ID field of a channel event that reports an RF
// event rather than a response to a command.
const msgRFEvent = 0x01

var msgNames = map[byte]string{
	MsgChannelEvent:             "ChannelEvent",
	MsgUnassignChannel:          "UnassignChannel",
	MsgAssignChannel:            "AssignChannel",
	MsgChannelPeriod:            "ChannelPeriod",
	MsgSearchTimeout:            "SearchTimeout",
	MsgRFFrequency:              "RFFrequency",
	MsgSetNetworkKey:            "SetNetworkKey",
	MsgResetSystem:              "ResetSystem",
	MsgOpenChannel:              "OpenChannel",
	MsgCloseChannel:             "CloseChannel",
	MsgRequest:                  "Request",
	MsgBroadcastData:            "BroadcastData",
	MsgAcknowledgedData:         "AcknowledgedData",
	MsgBurstData:                "BurstData",
	MsgChannelID:                "ChannelID",
	MsgCapabilities:             "Capabilities",
	MsgLowPrioritySearchTimeout: "LowPrioritySearchTimeout",
	MsgStartup:                  "Startup",
	MsgSerialError:              "SerialError",
}

func msgName(id byte) string {
	if s, ok := msgNames[id]; ok {
		return s
	}
	return fmt.Sprintf("0x%02X", id)
}

// ChannelType is the channel type used in Assign Channel.
type ChannelType byte

const (
	SlaveBidirectional  ChannelType = 0x00
	MasterBidirectional ChannelType = 0x10
)

// EventCode is a response or event code carried in a channel event message,
// "ANT Message Protocol and Usage" section 9.5.6.1.
type EventCode byte

const (
	ResponseNoError          EventCode = 0x00
	EventRxSearchTimeout     EventCode = 0x01
	EventRxFail              EventCode = 0x02
	EventTx                  EventCode = 0x03
	EventTransferRxFailed    EventCode = 0x04
	EventTransferTxCompleted EventCode = 0x05
	EventTransferTxFailed    EventCode = 0x06
	EventChannelClosed       EventCode = 0x07
	EventRxFailGoToSearch    EventCode = 0x08
	EventChannelCollision    EventCode = 0x09
	EventTransferTxStart     EventCode = 0x0A
	ChannelInWrongState      EventCode = 0x15
	ChannelNotOpened         EventCode = 0x16
	ChannelIDNotSet          EventCode = 0x18
	CloseAllChannels         EventCode = 0x19
	TransferInProgress       EventCode = 0x1F
	TransferSequenceError    EventCode = 0x20
	TransferInError          EventCode = 0x21
	MessageSizeExceedsLimit  EventCode = 0x27
	InvalidMessage           EventCode = 0x28
	InvalidNetworkNumber     EventCode = 0x29
	InvalidParameterProvided EventCode = 0x33
	EventSerialQueueOverflow EventCode = 0x34
	EventQueueOverflow       EventCode = 0x35
)

var eventNames = map[EventCode]string{
	ResponseNoError:          "RESPONSE_NO_ERROR",
	EventRxSearchTimeout:     "EVENT_RX_SEARCH_TIMEOUT",
	EventRxFail:              "EVENT_RX_FAIL",
	EventTx:                  "EVENT_TX",
	EventTransferRxFailed:    "EVENT_TRANSFER_RX_FAILED",
	EventTransferTxCompleted: "EVENT_TRANSFER_TX_COMPLETED",
	EventTransferTxFailed:    "EVENT_TRANSFER_TX_FAILED",
	EventChannelClosed:       "EVENT_CHANNEL_CLOSED",
	EventRxFailGoToSearch:    "EVENT_RX_FAIL_GO_TO_SEARCH",
	EventChannelCollision:    "EVENT_CHANNEL_COLLISION",
	EventTransferTxStart:     "EVENT_TRANSFER_TX_START",
	ChannelInWrongState:      "CHANNEL_IN_WRONG_STATE",
	ChannelNotOpened:         "CHANNEL_NOT_OPENED",
	ChannelIDNotSet:          "CHANNEL_ID_NOT_SET",
	CloseAllChannels:         "CLOSE_ALL_CHANNELS",
	TransferInProgress:       "TRANSFER_IN_PROGRESS",
	TransferSequenceError:    "TRANSFER_SEQUENCE_NUMBER_ERROR",
	TransferInError:          "TRANSFER_IN_ERROR",
	MessageSizeExceedsLimit:  "MESSAGE_SIZE_EXCEEDS_LIMIT",
	InvalidMessage:           "INVALID_MESSAGE",
	InvalidNetworkNumber:     "INVALID_NETWORK_NUMBER",
	InvalidParameterProvided: "INVALID_PARAMETER_PROVIDED",
	EventSerialQueueOverflow: "EVENT_SERIAL_QUE_OVERFLOW",
	EventQueueOverflow:       "EVENT_QUE_OVERFLOW",
}

func (c EventCode) String() string {
	if s, ok := eventNames[c]; ok {
		return s
	}
	return fmt.Sprintf("EVENT_0x%02X", byte(c))
}

// ChannelEvent is a decoded channel response or RF event message.
type ChannelEvent struct {
	Channel byte
	MsgID   byte // command being answered, or 0x01 for an RF event
	Code    EventCode
}

// IsRF reports whether the event is an RF event rather than a command response.
func (e ChannelEvent) IsRF() bool { return e.MsgID == msgRFEvent }

// ParseChannelEvent decodes a MsgChannelEvent message.
func ParseChannelEvent(m Message) (ChannelEvent, bool) {
	if m.ID != MsgChannelEvent || len(m.Data) < 3 {
		return ChannelEvent{}, false
	}
	return ChannelEvent{Channel: m.Data[0], MsgID: m.Data[1], Code: EventCode(m.Data[2])}, true
}

// ResponseError is a non-zero response code to a command.
type ResponseError struct {
	Msg  byte
	Code EventCode
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("ant: %s rejected: %s", msgName(e.Msg), e.Code)
}
